// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/protobuf"
	"k8s.io/apimachinery/pkg/util/sets"
	discoveryfake "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
	ctrl "sigs.k8s.io/controller-runtime"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/controllers"
	"github.com/projectcapsule/capsule-proxy/internal/indexer"
	"github.com/projectcapsule/capsule-proxy/internal/modules/clusterscoped"
	"github.com/projectcapsule/capsule-proxy/internal/modules/namespace"
	"github.com/projectcapsule/capsule-proxy/internal/modules/tenants"
	"github.com/projectcapsule/capsule-proxy/internal/options"
	"github.com/projectcapsule/capsule-proxy/internal/request"
)

type globalAccessReader struct {
	client.Reader
	globalLists atomic.Int64
	otherLists  atomic.Int64
	failure     error
}

type globalAccessManager struct {
	ctrl.Manager
	cl client.Client
}

func (m globalAccessManager) GetClient() client.Client    { return m.cl }
func (m globalAccessManager) GetAPIReader() client.Reader { return m.cl }

func TestGlobalAccessListenerRuntimeWiring(t *testing.T) {
	isolateCapsuleIdentities(t)
	for _, mode := range []struct{ require, capsule bool }{{true, true}, {false, true}, {false, false}} {
		t.Run(fmt.Sprintf("require=%t/capsule=%t", mode.require, mode.capsule), func(t *testing.T) {
			f := newGlobalAccessFixture(t, mode.capsule, mode.require, true)
			transport := f.filter.reverseProxy.Transport
			listener, err := options.NewKube([]request.AuthType{request.TLSCertificate}, nil, nil, "", &rest.Config{Host: "https://api.example", BearerToken: "proxy-token"}, nil, "", false, nil, "", nil, nil, mode.require, mode.capsule)
			if err != nil {
				t.Fatal(err)
			}
			filter, err := NewKubeFilter(listener, nil, f.filter.gates, nil, f.client, globalAccessManager{cl: f.client})
			if err != nil {
				t.Fatal(err)
			}
			f.filter = filter.(*kubeFilter)
			f.filter.log = logr.Discard()
			f.filter.reverseProxy.Transport = transport
			f.grant(t, capsulerbac.UserOwner, "external", globalNamespaceGrant("tenant-b-ns"))
			f.routes()
			response := httptest.NewRecorder()
			f.handler.ServeHTTP(response, promotionRequest(t.Context(), "external"))
			want := 200
			if mode.require {
				want = 403
			}
			if response.Code != want {
				t.Fatalf("listener runtime status=%d want=%d %s", response.Code, want, response.Body.String())
			}
		})
	}
}

func (r *globalAccessReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if _, ok := list.(*v1beta1.GlobalProxySettingsList); ok {
		r.globalLists.Add(1)
		options := &client.ListOptions{}
		options.ApplyOptions(opts)
		if options.FieldSelector == nil || !strings.Contains(options.FieldSelector.String(), indexer.GlobalKindField+"=") {
			return errors.New("global lookup must use the subject index")
		}
		if r.failure != nil {
			return r.failure
		}
	} else {
		r.otherLists.Add(1)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.Reader.List(ctx, list, opts...)
}

type globalAccessWriter struct {
	client.Writer
	identity    authenticationv1.UserInfo
	tokens      atomic.Int64
	reviews     atomic.Int64
	reject      bool
	rejectToken bool
}

func (w *globalAccessWriter) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	switch review := obj.(type) {
	case *authenticationv1.TokenReview:
		w.tokens.Add(1)
		review.Status.Authenticated = !w.rejectToken
		review.Status.User = w.identity
		return nil
	case *authorizationv1.SubjectAccessReview:
		w.reviews.Add(1)
		review.Status.Allowed = !w.reject
		return nil
	}
	return w.Writer.Create(ctx, obj, opts...)
}

type globalAccessFixture struct {
	filter       *kubeFilter
	client       client.Client
	reader       *globalAccessReader
	writer       *globalAccessWriter
	handler      http.Handler
	forwarded    atomic.Int64
	native       atomic.Int64
	last         atomic.Pointer[http.Request]
	nativeStatus int
}

func newGlobalAccessFixture(t testing.TB, capsuleAvailable, require, gate bool) *globalAccessFixture {
	t.Helper()
	base := newPromotionFixture(t, 2, gate)
	f := &globalAccessFixture{filter: base.filter, client: base.client, nativeStatus: http.StatusForbidden}
	f.reader = &globalAccessReader{Reader: f.client}
	f.writer = &globalAccessWriter{Writer: f.client, identity: authenticationv1.UserInfo{Username: "external", Groups: []string{"external-team"}}}
	f.filter.reader, f.filter.managerReader, f.filter.writer = f.client, f.reader, f.writer
	f.filter.capsuleAvailable, f.filter.requireCapsuleUser = capsuleAvailable, require
	f.filter.authTypes = []request.AuthType{request.TLSCertificate, request.BearerToken}
	f.filter.namespacedResources = sets.New("/pods", "/configmaps")
	f.filter.clusterResources = []schema.GroupVersionResource{{Version: "v1", Resource: "namespaces"}, {Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}, {Group: "capsule.clastix.io", Version: "v1beta2", Resource: "tenants"}}
	f.filter.clusterResourceKinds = map[schema.GroupVersionResource]schema.GroupVersionKind{
		{Version: "v1", Resource: "namespaces"}:                                       {Version: "v1", Kind: "Namespace"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}: {Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
		{Group: "capsule.clastix.io", Version: "v1beta2", Resource: "tenants"}:        {Group: "capsule.clastix.io", Version: "v1beta2", Kind: "Tenant"},
	}
	f.filter.reverseProxy.Transport = promotionTransport(func(r *http.Request) (*http.Response, error) {
		f.last.Store(r.Clone(r.Context()))
		status := http.StatusOK
		body := `{"kind":"NamespaceList","apiVersion":"v1","items":[]}`
		if r.Header.Get("Impersonate-User") != "" {
			f.native.Add(1)
			status = f.nativeStatus
			body = `{"kind":"Status","apiVersion":"v1","reason":"Forbidden","code":403}`
		} else {
			f.forwarded.Add(1)
			if r.Header.Get("Authorization") != "Bearer proxy-token" || len(r.Header.Values("Impersonate-Group")) != 0 {
				return nil, errors.New("global forwarding lost its credential boundary")
			}
			selector, err := labels.Parse(r.URL.Query().Get("labelSelector"))
			if err != nil || (selector.Empty() && mux.Vars(r)["name"] == "") {
				return nil, fmt.Errorf("missing mandatory selector: %v", err)
			}
			items := []map[string]any{}
			for _, name := range []string{"tenant-a-ns", "tenant-b-ns", "kube-system", "tenant-a", "tenant-b", "global", "foreign-role"} {
				if selector.Matches(labels.Set{corev1.LabelMetadataName: name}) {
					items = append(items, map[string]any{"metadata": map[string]string{"name": name}})
				}
			}
			data, _ := json.Marshal(map[string]any{"kind": "List", "apiVersion": "v1", "items": items})
			body = string(data)
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}, "X-Upstream": {"preserved"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	f.routes()
	return f
}

func (f *globalAccessFixture) routes() {
	router := mux.NewRouter()
	router.Use(f.filter.reverseProxyMiddleware)
	if f.filter.capsuleAvailable {
		f.filter.registerModule(router, namespace.List(f.filter.roleBindingsReflector, f.filter.reader))
		f.filter.registerModule(router, namespace.Get(f.filter.roleBindingsReflector, f.filter.reader))
		f.filter.registerModule(router, tenants.List(f.filter.reader))
		f.filter.registerModule(router, tenants.Get(f.filter.reader))
	} else {
		f.filter.registerModule(router, clusterscoped.List(f.filter.reader, f.filter.writer, "/api/v1/namespaces"))
	}
	discovery := &discoveryfake.FakeDiscovery{Fake: &clienttesting.Fake{}}
	discovery.Resources = []*metav1.APIResourceList{{GroupVersion: "rbac.authorization.k8s.io/v1", APIResources: []metav1.APIResource{{Name: "clusterroles", Kind: "ClusterRole"}}}, {GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "namespaces", Kind: "Namespace"}}}}
	if !f.filter.capsuleAvailable {
		f.filter.registerModule(router, clusterscoped.Get(discovery, f.filter.reader, f.filter.writer, "/api/v1/namespaces/{name}"))
	}
	f.filter.registerModule(router, clusterscoped.List(f.filter.reader, f.filter.writer, "/apis/rbac.authorization.k8s.io/v1/clusterroles"))
	f.filter.registerModule(router, clusterscoped.Get(discovery, f.filter.reader, f.filter.writer, "/apis/rbac.authorization.k8s.io/v1/clusterroles/{name}"))
	f.filter.registerModule(router, clusterscoped.List(f.filter.reader, f.filter.writer, "/api/v1/pods"))
	router.PathPrefix("/").HandlerFunc(f.filter.impersonateHandler)
	f.handler = router
}

type globalReflectionCache struct {
	crcache.Cache
	builder *fake.ClientBuilder
	reader  client.Reader
	lists   int
}

func (c *globalReflectionCache) IndexField(_ context.Context, obj client.Object, field string, extract client.IndexerFunc) error {
	c.builder = c.builder.WithIndex(obj, field, extract)
	return nil
}

func (c *globalReflectionCache) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return c.reader.Get(ctx, key, obj, opts...)
}
func (c *globalReflectionCache) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	c.lists++
	return c.reader.List(ctx, list, opts...)
}

func TestGlobalAccessDoesNotInheritTenantReflection(t *testing.T) {
	isolateCapsuleIdentities(t)
	f := newGlobalAccessFixture(t, true, false, true)
	f.grant(t, capsulerbac.GroupOwner, "external-team", globalNamespaceGrant("tenant-b-ns"))
	binding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "reflected", Namespace: "tenant-a-ns", Labels: map[string]string{controllers.RoleBindingReflectionLabel: "true"}}, Subjects: []rbacv1.Subject{{Kind: "Group", APIGroup: rbacv1.GroupName, Name: "external-team"}}, RoleRef: rbacv1.RoleRef{Kind: "Role", APIGroup: rbacv1.GroupName, Name: "reader"}}
	cache := &globalReflectionCache{builder: fake.NewClientBuilder().WithScheme(f.client.Scheme()).WithObjects(binding, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "reader", Namespace: "tenant-a-ns"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"list"}}}})}
	reflector, err := controllers.NewRoleBindingReflector(t.Context(), cache)
	if err != nil {
		t.Fatal(err)
	}
	cache.reader = cache.builder.Build()
	f.filter.roleBindingsReflector = reflector
	f.routes()
	for _, capsuleUser := range []bool{false, true} {
		if capsuleUser {
			controllers.CapsuleUsers.Insert("external")
		}
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, promotionRequest(t.Context(), "external", "external-team"))
		if response.Code != 200 || !strings.Contains(response.Body.String(), `"name":"tenant-b-ns"`) || strings.Contains(response.Body.String(), `"name":"tenant-a-ns"`) != capsuleUser {
			t.Fatalf("Capsule=%t: %d %s", capsuleUser, response.Code, response.Body.String())
		}
		if !capsuleUser && cache.lists != 0 {
			t.Fatal("global-only request consulted tenant reflection")
		}
		if capsuleUser && cache.lists == 0 {
			t.Fatal("existing Capsule reflection was lost")
		}
	}
}

func (f *globalAccessFixture) grant(t testing.TB, kind capsulerbac.OwnerKind, subject string, resources []v1beta1.ClusterResource) *v1beta1.GlobalProxySettings {
	t.Helper()
	setting := &v1beta1.GlobalProxySettings{ObjectMeta: metav1.ObjectMeta{Name: "external-grant"}, Spec: v1beta1.GlobalProxySettingsSpec{Rules: []v1beta1.GlobalSubjectSpec{{Subjects: []v1beta1.GlobalSubject{{Kind: kind, Name: subject}}, ClusterResources: resources}}}}
	if err := f.client.Create(t.Context(), setting); err != nil {
		t.Fatal(err)
	}
	return setting
}

func globalNamespaceGrant(name string) []v1beta1.ClusterResource {
	return []v1beta1.ClusterResource{{APIGroups: []string{""}, Resources: []string{"namespaces"}, Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: corev1.LabelMetadataName, Operator: metav1.LabelSelectorOpIn, Values: []string{name}}}}}}
}

func TestGlobalAccessPipeline(t *testing.T) {
	isolateCapsuleIdentities(t)
	for _, capsuleAvailable := range []bool{true, false} {
		for _, mode := range []string{"group", "principal-set", "namespace-get", "cluster-list", "cluster-get", "user", "serviceaccount", "wrong-sa-namespace", "user-group-collision", "unmatched", "default", "gate-disabled", "wrong-resource", "wrong-group", "get-only-list", "empty-selector", "invalid-selector", "conflicting-selector", "no-selected-objects", "ignored-group", "ignored-user", "allowed-path", "watch", "malformed-watch", "mutation", "namespaced", "lookup-error", "cancelled", "native-allowed", "capsule-owner"} {
			t.Run(fmt.Sprintf("capsule=%t/%s", capsuleAvailable, mode), func(t *testing.T) {
				f := newGlobalAccessFixture(t, capsuleAvailable, mode == "default", mode != "gate-disabled")
				kind, subject := capsulerbac.GroupOwner, "external-team"
				user, groups := "external", []string{"external-team"}
				resources := globalNamespaceGrant("tenant-b-ns")
				path, method := "/api/v1/namespaces", http.MethodGet
				wantStatus, wantFiltered := 200, true
				wantNames := []string{"tenant-b-ns"}
				switch mode {
				case "principal-set":
					subject = "principalSet://iam.googleapis.com/locations/global/workforcePools/test-pool/group/test-group"
					groups = []string{subject}
				case "namespace-get":
					path += "/tenant-b-ns"
				case "cluster-list", "cluster-get":
					resources[0].APIGroups, resources[0].Resources = []string{"rbac.authorization.k8s.io"}, []string{"clusterroles"}
					resources[0].Selector.MatchExpressions[0].Values = []string{"foreign-role"}
					path = "/apis/rbac.authorization.k8s.io/v1/clusterroles"
					if mode == "cluster-get" {
						path += "/foreign-role"
					}
					wantNames = []string{"foreign-role"}
				case "user":
					kind, subject = capsulerbac.UserOwner, user
				case "serviceaccount":
					kind, subject, user = capsulerbac.ServiceAccountOwner, "system:serviceaccount:outside:reader", "system:serviceaccount:outside:reader"
				case "wrong-sa-namespace":
					kind, subject, user = capsulerbac.ServiceAccountOwner, "system:serviceaccount:outside:reader", "system:serviceaccount:other:reader"
					wantStatus, wantFiltered = 403, false
				case "user-group-collision":
					user, groups = subject, nil
					wantStatus, wantFiltered = 403, false
				case "unmatched":
					subject = "another-team"
					wantStatus, wantFiltered = 403, false
				case "default", "gate-disabled":
					wantStatus, wantFiltered = 403, false
				case "wrong-resource":
					resources[0].Resources = []string{"nodes"}
					wantStatus, wantFiltered = 403, false
				case "wrong-group":
					resources[0].APIGroups = []string{"apps"}
					wantStatus, wantFiltered = 403, false
				case "get-only-list":
					resources[0].Operations = []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationGet}
					wantStatus, wantFiltered = 403, false
				case "empty-selector":
					resources[0].Selector = &metav1.LabelSelector{}
					wantStatus, wantFiltered = 403, false
				case "invalid-selector":
					resources[0].Selector.MatchExpressions[0].Operator = "Invalid"
					wantStatus, wantFiltered = 403, false
				case "conflicting-selector":
					path += "?labelSelector=kubernetes.io%2Fmetadata.name%3Dtenant-a-ns"
					wantNames = nil
				case "no-selected-objects":
					resources = globalNamespaceGrant("missing")
					wantNames = nil
				case "ignored-group":
					f.filter.ignoredUserGroups = sets.New(subject)
					wantStatus, wantFiltered = 403, false
				case "ignored-user":
					f.filter.ignoredUsernames = sets.New(user)
					wantStatus, wantFiltered = 403, false
				case "allowed-path":
					f.filter.allowedPaths = sets.New(path)
					wantStatus, wantFiltered = 403, false
				case "watch":
					path += "?watch=true"
					wantStatus, wantFiltered = 403, false
				case "malformed-watch":
					path += "?watch=invalid"
					wantStatus, wantFiltered = 403, false
				case "mutation":
					method = http.MethodPost
					wantStatus, wantFiltered = 403, false
				case "namespaced":
					resources[0].Resources = []string{"*"}
					path = "/api/v1/pods"
					wantStatus, wantFiltered = 403, false
				case "lookup-error":
					f.reader.failure = errors.New("global cache unavailable")
					wantStatus, wantFiltered = 500, false
				case "cancelled":
					wantStatus, wantFiltered = 500, false
				case "native-allowed":
					subject = "another-team"
					f.nativeStatus = 201
					wantStatus, wantFiltered = 201, false
				case "capsule-owner":
					if !capsuleAvailable {
						t.Skip("Capsule ownership requires Capsule integration")
					}
					user, groups = promotedUsername, nil
					wantNames = []string{"tenant-a-ns"}
				}
				f.grant(t, kind, subject, resources)
				f.routes()
				r := promotionRequest(t.Context(), user, groups...)
				r.Method = method
				u := httptest.NewRequest(method, path, nil).URL
				r.URL = u
				if mode == "cancelled" {
					ctx, cancel := context.WithCancel(r.Context())
					cancel()
					r = r.WithContext(ctx)
				}
				response := httptest.NewRecorder()
				f.handler.ServeHTTP(response, r)
				if response.Code != wantStatus || (f.forwarded.Load() > 0) != wantFiltered {
					t.Fatalf("status=%d filtered=%d native=%d body=%s", response.Code, f.forwarded.Load(), f.native.Load(), response.Body.String())
				}
				if wantFiltered && mode != "namespace-get" && mode != "cluster-get" {
					var list corev1.NamespaceList
					if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
						t.Fatal(err)
					}
					names := sets.New[string]()
					for _, ns := range list.Items {
						names.Insert(ns.Name)
					}
					if !names.Equal(sets.New(wantNames...)) {
						t.Fatalf("visible names=%v want=%v", names, wantNames)
					}
				}
				if mode != "capsule-owner" && (!capsuleAvailable || kind != capsulerbac.ServiceAccountOwner) && f.reader.otherLists.Load() != 0 {
					t.Fatalf("non-Capsule request performed %d tenant/settings lookups", f.reader.otherLists.Load())
				}
				if mode == "lookup-error" || mode == "cancelled" {
					if f.native.Load() != 0 {
						t.Fatal("lookup failure fell through to native forwarding")
					}
				} else if response.Header().Get("X-Upstream") != "preserved" {
					t.Fatal("upstream header was lost")
				}
				if mode == "default" || mode == "gate-disabled" || mode == "ignored-group" || mode == "ignored-user" || mode == "allowed-path" || mode == "watch" || mode == "malformed-watch" || mode == "mutation" || mode == "namespaced" {
					if f.reader.globalLists.Load() != 0 {
						t.Fatal("irrelevant request performed global settings lookups")
					}
				}
			})
		}
	}
}

func TestGlobalAccessRevocationAndIdentityReuse(t *testing.T) {
	isolateCapsuleIdentities(t)
	f := newGlobalAccessFixture(t, true, false, true)
	setting := f.grant(t, capsulerbac.GroupOwner, "external-team", globalNamespaceGrant("tenant-b-ns"))
	f.filter.authTypes = []request.AuthType{request.BearerToken}
	f.routes()
	for i, revoke := range []bool{false, true} {
		if revoke {
			if err := f.client.Delete(t.Context(), setting); err != nil {
				t.Fatal(err)
			}
		}
		r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces?fieldSelector=metadata.name%3Dtenant-b-ns&limit=1&resourceVersion=0", nil)
		r.Header.Set("Authorization", "Bearer caller-token")
		r.Header.Set("Impersonate-User", "external")
		r.Header.Add("Impersonate-Group", "external-team")
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, r)
		want := 200
		if revoke {
			want = 403
		}
		if response.Code != want {
			t.Fatalf("revoked=%t: status=%d %s", revoke, response.Code, response.Body.String())
		}
		upstream := f.last.Load()
		if upstream.URL.Query().Get("fieldSelector") != "metadata.name=tenant-b-ns" || upstream.URL.Query().Get("limit") != "1" || upstream.URL.Query().Get("resourceVersion") != "0" {
			t.Fatal("upstream query was changed")
		}
		if f.writer.tokens.Load() != int64(2*(i+1)) || f.writer.reviews.Load() != int64(2*(i+1)) {
			t.Fatalf("duplicate authentication/reviews: tokens=%d reviews=%d", f.writer.tokens.Load(), f.writer.reviews.Load())
		}
	}
}

func TestGlobalAccessRequiresVerifiedIdentity(t *testing.T) {
	isolateCapsuleIdentities(t)
	for _, mode := range []string{"unauthenticated", "unverified-token", "denied-impersonation"} {
		t.Run(mode, func(t *testing.T) {
			f := newGlobalAccessFixture(t, true, false, true)
			f.grant(t, capsulerbac.GroupOwner, "external-team", globalNamespaceGrant("tenant-b-ns"))
			r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", nil)
			if mode == "unverified-token" {
				f.writer.rejectToken = true
				r.Header.Set("Authorization", "Bearer unverified-token")
			}
			if mode == "denied-impersonation" {
				f.writer.reject = true
				r.Header.Set("Authorization", "Bearer caller-token")
				r.Header.Set("Impersonate-User", "external")
				r.Header.Set("Impersonate-Group", "external-team")
			}
			response := httptest.NewRecorder()
			f.handler.ServeHTTP(response, r)
			if response.Code != 403 || f.reader.globalLists.Load() != 0 || f.forwarded.Load() != 0 || f.native.Load() != 0 {
				t.Fatalf("unverified request reached access resolution: status=%d lookups=%d filtered=%d native=%d", response.Code, f.reader.globalLists.Load(), f.forwarded.Load(), f.native.Load())
			}
		})
	}
}

func TestGlobalAccessUsesManagerIndexesWithDirectResourceReader(t *testing.T) {
	isolateCapsuleIdentities(t)
	f := newGlobalAccessFixture(t, true, false, true)
	f.grant(t, capsulerbac.UserOwner, "external", globalNamespaceGrant("tenant-b-ns"))
	// A direct resource reader has no synthetic manager indexes.
	direct := &globalAccessReader{Reader: fake.NewClientBuilder().WithScheme(f.client.Scheme()).WithObjects(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-b-ns", Labels: map[string]string{corev1.LabelMetadataName: "tenant-b-ns"}}}).Build()}
	f.filter.reader = direct
	f.routes()
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, promotionRequest(t.Context(), "external"))
	if response.Code != 200 || f.reader.globalLists.Load() != 1 || f.reader.otherLists.Load() != 0 || direct.otherLists.Load() != 0 || direct.globalLists.Load() != 0 || !strings.Contains(response.Body.String(), `"name":"tenant-b-ns"`) {
		t.Fatalf("global indexed lookup used the wrong reader: status=%d global=%d other=%d body=%s", response.Code, f.reader.globalLists.Load(), f.reader.otherLists.Load(), response.Body.String())
	}
}

func TestGlobalAccessTenantNameIsolation(t *testing.T) {
	isolateCapsuleIdentities(t)
	controllers.CapsuleUsers.Insert("external")
	f := newGlobalAccessFixture(t, true, false, true)
	f.grant(t, capsulerbac.UserOwner, "external", []v1beta1.ClusterResource{{APIGroups: []string{"capsule.clastix.io"}, Resources: []string{"tenants"}, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: "tenant-b"}}}})
	if err := f.client.Create(t.Context(), &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "global", Labels: map[string]string{corev1.LabelMetadataName: "global"}}}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/apis/capsule.clastix.io/v1beta2/tenants", "/apis/capsule.clastix.io/v1beta2/tenants/global"} {
		r := promotionRequest(t.Context(), "external")
		r.URL = httptest.NewRequest(http.MethodGet, path, nil).URL
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, r)
		if strings.HasSuffix(path, "/global") {
			if response.Code != 404 {
				t.Fatalf("unselected tenant global: %d %s", response.Code, response.Body.String())
			}
		} else if response.Code != 200 || strings.Contains(response.Body.String(), `"name":"global"`) || !strings.Contains(response.Body.String(), `"name":"tenant-b"`) {
			t.Fatalf("tenant isolation: %d %s", response.Code, response.Body.String())
		}
	}
}

func (f *globalAccessFixture) enableAuthorization(t testing.TB) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := authorizationv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	f.filter.scheme = scheme
	f.filter.universalDecoder = serializer.NewCodecFactory(scheme).UniversalDeserializer()
	f.filter.protoEncoder = protobuf.NewSerializer(scheme, scheme)
	f.handler = f.filter.authorizationMiddleware(f.handler)
}

func TestGlobalAccessSelfReviews(t *testing.T) {
	isolateCapsuleIdentities(t)
	for _, mediaType := range []string{"application/json", "application/vnd.kubernetes.protobuf"} {
		for _, mode := range []string{"namespace", "named-selected", "named-excluded", "named-missing", "get-only-list", "get-only-get", "namespaced", "subresource", "mutation", "unknown-resource", "wrong-version", "wrong-group", "unmatched", "default", "ignored", "gate-disabled"} {
			t.Run(mediaType+"/"+mode, func(t *testing.T) {
				f := newGlobalAccessFixture(t, true, mode == "default", mode != "gate-disabled")
				resources := globalNamespaceGrant("tenant-b-ns")
				attributes := &authorizationv1.ResourceAttributes{Version: "v1", Resource: "namespaces", Verb: "list"}
				want := mode == "namespace" || mode == "get-only-get" || mode == "named-selected"
				if strings.HasPrefix(mode, "named-") {
					attributes.Verb = "get"
					attributes.Name = "tenant-b-ns"
				}
				if mode == "named-excluded" {
					attributes.Name = "tenant-a-ns"
				}
				if mode == "named-missing" {
					attributes.Name = "missing"
				}
				if strings.HasPrefix(mode, "get-only") {
					resources[0].Operations = []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationGet}
				}
				if mode == "get-only-get" {
					attributes.Verb = "get"
				}
				if mode == "namespaced" {
					attributes.Resource = "pods"
					resources[0].Resources = []string{"*"}
				}
				if mode == "subresource" {
					attributes.Subresource = "status"
				}
				if mode == "mutation" {
					attributes.Verb = "delete"
				}
				if mode == "unknown-resource" {
					attributes.Resource = "nonexistent"
					resources[0].Resources = []string{"*"}
				}
				if mode == "wrong-version" {
					attributes.Version = "v99"
				}
				if mode == "wrong-group" {
					attributes.Group = "other.example.com"
					resources[0].APIGroups = []string{"*"}
				}
				subject := "external-team"
				if mode == "unmatched" {
					subject = "another-team"
				}
				f.grant(t, capsulerbac.GroupOwner, subject, resources)
				if mode == "ignored" {
					f.filter.ignoredUserGroups = sets.New(subject)
				}
				f.enableAuthorization(t)
				review := &authorizationv1.SelfSubjectAccessReview{TypeMeta: metav1.TypeMeta{APIVersion: "authorization.k8s.io/v1", Kind: "SelfSubjectAccessReview"}, Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: attributes}, Status: authorizationv1.SubjectAccessReviewStatus{Denied: true, Reason: "native RBAC"}}
				var body []byte
				var err error
				if mediaType == "application/json" {
					body, err = json.Marshal(review)
				} else {
					body, err = runtime.Encode(f.filter.protoEncoder, review)
				}
				if err != nil {
					t.Fatal(err)
				}
				f.filter.reverseProxy.Transport = promotionTransport(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 201, Header: http.Header{"Content-Type": {mediaType}, "X-Upstream": {"preserved"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
				})
				r := httptest.NewRequest(http.MethodPost, "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", nil)
				r.Header.Set("Authorization", "Bearer caller-token")
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Accept", mediaType)
				response := httptest.NewRecorder()
				f.handler.ServeHTTP(response, r)
				if response.Code != 201 || response.Header().Get("X-Upstream") != "preserved" {
					t.Fatalf("response=%d %s", response.Code, response.Body.String())
				}
				obj, _, err := f.filter.universalDecoder.Decode(response.Body.Bytes(), nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				got := obj.(*authorizationv1.SelfSubjectAccessReview)
				if got.Status.Allowed != want || got.Status.Denied == want {
					t.Fatalf("self review status=%+v want allowed=%t", got.Status, want)
				}
				if f.writer.tokens.Load() != 1 {
					t.Fatalf("token reviewed %d times", f.writer.tokens.Load())
				}
				if !want && (mode == "default" || mode == "ignored" || mode == "gate-disabled") && f.reader.globalLists.Load() != 0 {
					t.Fatal("ineligible self review performed global lookup")
				}
			})
		}
	}
}

func TestGlobalAccessSelfReviewPreservesUpstreamErrors(t *testing.T) {
	isolateCapsuleIdentities(t)
	for _, body := range []string{`{"kind":"Status","apiVersion":"v1","reason":"Forbidden","code":403}`, "upstream unavailable"} {
		f := newGlobalAccessFixture(t, true, false, true)
		f.grant(t, capsulerbac.GroupOwner, "external-team", globalNamespaceGrant("tenant-b-ns"))
		f.enableAuthorization(t)
		f.filter.reverseProxy.Transport = promotionTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 503, Header: http.Header{"Content-Type": {"application/json"}, "Retry-After": {"5"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})
		r := httptest.NewRequest(http.MethodPost, "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", nil)
		r.Header.Set("Authorization", "Bearer caller-token")
		r.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, r)
		if response.Code != 503 || response.Body.String() != body || response.Header().Get("Retry-After") != "5" {
			t.Fatalf("upstream error changed: %d %s", response.Code, response.Body.String())
		}
	}
}
