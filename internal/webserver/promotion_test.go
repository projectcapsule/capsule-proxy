// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/controllers"
	"github.com/projectcapsule/capsule-proxy/internal/indexer"
	namespacemodule "github.com/projectcapsule/capsule-proxy/internal/modules/namespace"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/webserver/middleware"
)

const promotedUsername = "system:serviceaccount:tenant-a-ns:reader"

type promotionCountingReader struct {
	client.Reader
	ownerLists atomic.Int64
	totalLists atomic.Int64
	failure    error
}

func (r *promotionCountingReader) List(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
	r.totalLists.Add(1)
	options := &client.ListOptions{}
	options.ApplyOptions(opts)
	if _, ok := obj.(*capsulev1beta2.TenantList); ok && options.FieldSelector.Matches(labels.Set{indexer.TenantOwnerKindField: "ServiceAccount:" + promotedUsername}) {
		r.ownerLists.Add(1)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.failure != nil {
		return r.failure
	}
	return r.Reader.List(ctx, obj, opts...)
}

type promotionReviewWriter struct {
	client.Writer
	reviews atomic.Int64
	reject  bool
}

func (w *promotionReviewWriter) Create(ctx context.Context, object client.Object, opts ...client.CreateOption) error {
	if review, ok := object.(*authenticationv1.TokenReview); ok {
		w.reviews.Add(1)
		review.Status.Authenticated = !w.reject
		review.Status.User = authenticationv1.UserInfo{Username: promotedUsername, Groups: []string{"system:authenticated", "system:serviceaccounts", "system:serviceaccounts:tenant-a-ns"}}
		return nil
	}
	return w.Writer.Create(ctx, object, opts...)
}

type promotionTransport func(*http.Request) (*http.Response, error)

func (f promotionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type promotionFixture struct {
	filter    *kubeFilter
	client    client.Client
	reader    *promotionCountingReader
	reviews   *promotionReviewWriter
	handler   http.Handler
	forwarded atomic.Int64
	passed    atomic.Int64
}

func newPromotionFixture(t testing.TB, tenants int, gate bool) *promotionFixture {
	t.Helper()
	access := newProxySettingAccessFixture(t, gate, capsulerbac.ServiceAccountOwner, promotedUsername, true, 0)
	for i := 2; i < tenants; i++ {
		other := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("unrelated-%d", i)}}
		other.Status.Owners = capsulerbac.OwnerStatusListSpec{{UserSpec: capsulerbac.UserSpec{Kind: capsulerbac.ServiceAccountOwner, Name: fmt.Sprintf("system:serviceaccount:unrelated-%d:reader", i)}}}
		if err := access.client.Create(t.Context(), other); err != nil {
			t.Fatal(err)
		}
	}
	r := &promotionCountingReader{Reader: access.client}
	f := &promotionFixture{filter: access.filter, client: access.client, reader: r}
	f.reviews = &promotionReviewWriter{Writer: f.filter.writer}
	f.filter.writer = f.reviews
	f.filter.managerReader = r
	f.filter.authTypes = []request.AuthType{request.TLSCertificate}
	f.filter.usernameClaimField = "preferred_username"
	endpoint, _ := url.Parse("https://api.example")
	f.filter.reverseProxy = httputil.NewSingleHostReverseProxy(endpoint)
	f.filter.reverseProxy.Transport = promotionTransport(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Impersonate-User") != "" {
			return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"kind":"Status","apiVersion":"v1","reason":"Forbidden","message":"native Kubernetes authorization denied namespace listing","code":403}`)), Request: req}, nil
		}
		f.forwarded.Add(1)
		if req.Header.Get("Authorization") != "Bearer proxy-token" || req.Header.Get("Impersonate-User") != "" || len(req.Header.Values("Impersonate-Group")) != 0 {
			return nil, fmt.Errorf("filtered request used incorrect credentials")
		}
		selector, err := labels.Parse(req.URL.Query().Get("labelSelector"))
		if err != nil {
			return nil, err
		}
		if selector.Empty() {
			return nil, errors.New("filtered request lost its selector")
		}
		list := &corev1.NamespaceList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "NamespaceList"}, Items: []corev1.Namespace{}}
		for _, name := range []string{"tenant-a-ns", "tenant-b-ns", "kube-system"} {
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{corev1.LabelMetadataName: name, "dontexistsignoreme": "true"}}}
			if selector.Matches(labels.Set(ns.Labels)) {
				list.Items = append(list.Items, ns)
			}
		}
		body, err := json.Marshal(list)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
	})
	next := f.filter.moduleHandler(namespacemodule.List(nil, f.filter.reader))
	f.handler = middleware.CheckUserInCapsuleGroupMiddleware(f.filter.writer, r, logr.Discard(), "", f.filter.authTypes, nil, nil, false, "", func(w http.ResponseWriter, req *http.Request) {
		f.passed.Add(1)
		f.filter.impersonateHandler(w, req)
	})(next)
	f.handler = middleware.CheckJWTMiddleware(f.reviews)(f.handler)
	f.handler = middleware.CheckJWTMiddleware(f.reviews)(f.handler)
	f.handler = f.filter.reverseProxyMiddleware(f.handler)
	return f
}

func promotionRequest(ctx context.Context, user string, groups ...string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "https://proxy.example/api/v1/namespaces", nil).WithContext(ctx)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: user, Organization: groups}}}}
	return req
}

func isolateCapsuleIdentities(t testing.TB) {
	t.Helper()
	users, groups := controllers.CapsuleUsers, controllers.CapsuleUserGroups
	controllers.CapsuleUsers = sets.New[string]()
	controllers.CapsuleUserGroups = sets.New[string]()
	t.Cleanup(func() { controllers.CapsuleUsers, controllers.CapsuleUserGroups = users, groups })
}

func TestPromotedOwnerNamespacePipeline(t *testing.T) {
	isolateCapsuleIdentities(t)
	for _, gate := range []bool{false, true} {
		for _, mode := range []string{"owner", "empty", "configured-empty", "selector-empty", "malformed-serviceaccount", "different-sa-namespace", "different-sa-name", "user-kind-collision", "unconfigured-user", "configured-user", "configured-group", "missing-identity", "lookup-error", "cancelled"} {
			t.Run(fmt.Sprintf("gate=%v/%s", gate, mode), func(t *testing.T) {
				f := newPromotionFixture(t, 2, gate)
				user := promotedUsername
				var groups []string
				expected := http.StatusOK
				expectedNames := []string{"tenant-a-ns"}
				switch mode {
				case "empty", "configured-empty":
					tnt := &capsulev1beta2.Tenant{}
					if err := f.client.Get(t.Context(), client.ObjectKey{Name: "tenant-a"}, tnt); err != nil {
						t.Fatal(err)
					}
					tnt.Status.Namespaces = nil
					if err := f.client.Update(t.Context(), tnt); err != nil {
						t.Fatal(err)
					}
					expectedNames = nil
					if mode == "configured-empty" {
						controllers.CapsuleUsers.Insert(user)
					}
				case "selector-empty":
					expectedNames = nil
				case "different-sa-namespace":
					user = "system:serviceaccount:tenant-b-ns:reader"
					expected = 403
				case "different-sa-name":
					user = "system:serviceaccount:tenant-a-ns:other"
					expected = 403
				case "user-kind-collision":
					tnt := &capsulev1beta2.Tenant{}
					if err := f.client.Get(t.Context(), client.ObjectKey{Name: "tenant-a"}, tnt); err != nil {
						t.Fatal(err)
					}
					tnt.Status.Owners[0].Kind = capsulerbac.UserOwner
					if err := f.client.Update(t.Context(), tnt); err != nil {
						t.Fatal(err)
					}
					expected = 403
				case "malformed-serviceaccount":
					user = "system:serviceaccount:missing-name"
					expected = 403
				case "unconfigured-user":
					user = "ordinary-user"
					expected = 403
				case "configured-user":
					controllers.CapsuleUsers.Insert(user)
				case "configured-group":
					groups = []string{"capsule-users"}
					controllers.CapsuleUserGroups.Insert("capsule-users")
				case "missing-identity":
					expected = 403
				case "lookup-error":
					f.reader.failure = errors.New("owner index unavailable")
					expected = 500
				case "cancelled":
					expected = 500
				}
				req := promotionRequest(t.Context(), user, groups...)
				if mode == "missing-identity" {
					req.TLS = nil
				}
				if mode == "cancelled" {
					ctx, cancel := context.WithCancel(req.Context())
					cancel()
					req = req.WithContext(ctx)
				}
				if mode == "selector-empty" {
					query := req.URL.Query()
					query.Set("labelSelector", corev1.LabelMetadataName+"=tenant-b-ns")
					req.URL.RawQuery = query.Encode()
				}
				response := httptest.NewRecorder()
				f.handler.ServeHTTP(response, req)
				if response.Code != expected {
					t.Fatalf("response = %d: %s, want %d", response.Code, response.Body.String(), expected)
				}
				if expected == 200 {
					var list corev1.NamespaceList
					if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
						t.Fatal(err)
					}
					names := make([]string, 0, len(list.Items))
					for _, ns := range list.Items {
						names = append(names, ns.Name)
					}
					if len(names) != len(expectedNames) || (len(names) == 1 && names[0] != expectedNames[0]) {
						t.Fatalf("namespaces = %v, want %v", names, expectedNames)
					}
					if f.forwarded.Load() != 1 || f.passed.Load() != 0 {
						t.Fatalf("forwarded=%d passed=%d", f.forwarded.Load(), f.passed.Load())
					}
					if f.reader.ownerLists.Load() != 1 {
						t.Fatalf("owner index read %d times; request snapshot must be reused", f.reader.ownerLists.Load())
					}
				} else {
					if f.forwarded.Load() != 0 {
						t.Fatal("denied request reached privileged forwarding")
					}
					if (mode == "lookup-error" || mode == "cancelled" || mode == "missing-identity") && f.passed.Load() != 0 {
						t.Fatal("dependency/authentication failure fell through to native authorization")
					}
				}
				if mode == "unconfigured-user" || mode == "missing-identity" || mode == "malformed-serviceaccount" {
					if f.reader.totalLists.Load() != 0 {
						t.Fatal("irrelevant identity triggered ownership lookups")
					}
				}
				controllers.CapsuleUsers = sets.New[string]()
				controllers.CapsuleUserGroups = sets.New[string]()
			})
		}
	}
}

func TestPromotedOwnerRevocationAndNewRequests(t *testing.T) {
	isolateCapsuleIdentities(t)
	f := newPromotionFixture(t, 2, true)
	for _, owned := range []bool{true, false, true} {
		tnt := &capsulev1beta2.Tenant{}
		if err := f.client.Get(t.Context(), client.ObjectKey{Name: "tenant-a"}, tnt); err != nil {
			t.Fatal(err)
		}
		tnt.Status.Owners = nil
		if owned {
			tnt.Status.Owners = capsulerbac.OwnerStatusListSpec{{UserSpec: capsulerbac.UserSpec{Kind: capsulerbac.ServiceAccountOwner, Name: promotedUsername}}}
		}
		if err := f.client.Update(t.Context(), tnt); err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, promotionRequest(t.Context(), promotedUsername))
		expected := 403
		if owned {
			expected = 200
		}
		if response.Code != expected {
			t.Fatalf("owned=%v: status=%d body=%s", owned, response.Code, response.Body.String())
		}
	}
	if f.reader.ownerLists.Load() != 3 {
		t.Fatal("ownership must refresh for every new request, while reusing each snapshot within its request")
	}
}

func TestPromotedOwnerTokenReviewReuse(t *testing.T) {
	isolateCapsuleIdentities(t)
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprintf("reject=%v", reject), func(t *testing.T) {
			f := newPromotionFixture(t, 2, true)
			f.filter.authTypes[0] = request.BearerToken
			f.reviews.reject = reject
			req := httptest.NewRequest(http.MethodGet, "https://proxy.example/api/v1/namespaces", nil)
			req.Header.Set("Authorization", "Bearer verified-test-token")
			response := httptest.NewRecorder()
			f.handler.ServeHTTP(response, req)
			if f.reviews.reviews.Load() != 3 {
				t.Fatalf("TokenReview calls=%d, want the existing two JWT checks plus one reused identity resolution", f.reviews.reviews.Load())
			}
			if reject {
				if response.Code == http.StatusOK || f.reader.totalLists.Load() != 0 || f.forwarded.Load() != 0 || f.passed.Load() != 0 {
					t.Fatal("unverified token reached ownership lookup or forwarding")
				}
			} else if response.Code != http.StatusOK || f.reader.ownerLists.Load() != 1 || f.forwarded.Load() != 1 {
				t.Fatalf("status=%d owner lookups=%d upstream=%d body=%s", response.Code, f.reader.ownerLists.Load(), f.forwarded.Load(), response.Body.String())
			}
		})
	}
}
