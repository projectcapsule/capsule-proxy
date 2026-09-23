// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	capsuleindexer "github.com/projectcapsule/capsule/pkg/runtime/indexers"
	tenantindexer "github.com/projectcapsule/capsule/pkg/runtime/indexers/tenant"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	discoveryfake "k8s.io/client-go/discovery/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/component-base/featuregate"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/authorization"
	"github.com/projectcapsule/capsule-proxy/internal/features"
	"github.com/projectcapsule/capsule-proxy/internal/indexer"
	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/modules/clusterscoped"
	moduleerrors "github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	namespacemodule "github.com/projectcapsule/capsule-proxy/internal/modules/namespace"
	"github.com/projectcapsule/capsule-proxy/internal/modules/tenants"
	"github.com/projectcapsule/capsule-proxy/internal/request"
)

type proxySettingAccessFixture struct {
	filter  *kubeFilter
	client  *proxySettingCountingClient
	setting *v1beta1.ProxySetting
}

type proxySettingCountingClient struct {
	client.Client
	namespaceLists int
}

func (c *proxySettingCountingClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if _, ok := list.(*corev1.NamespaceList); ok {
		c.namespaceLists++
	}
	return c.Client.List(ctx, list, opts...)
}

func newProxySettingAccessFixture(t testing.TB, gate bool, kind capsulerbac.OwnerKind, subject string, owner bool, unrelated int) proxySettingAccessFixture {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, rbacv1.AddToScheme, capsulev1beta2.AddToScheme, v1beta1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	tenantA := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a", Labels: map[string]string{corev1.LabelMetadataName: "tenant-a"}}}
	tenantA.Status.Namespaces = []string{"tenant-a-ns"}
	if owner {
		tenantA.Status.Owners = capsulerbac.OwnerStatusListSpec{{UserSpec: capsulerbac.UserSpec{Kind: kind, Name: subject}}}
	}
	setting := &v1beta1.ProxySetting{
		ObjectMeta: metav1.ObjectMeta{Name: "self-grant", Namespace: "tenant-a-ns"},
		Spec: v1beta1.ProxySettingSpec{Subjects: []v1beta1.OwnerSpec{{
			Kind: kind, Name: subject,
			ClusterResources: []v1beta1.ClusterResource{{
				APIGroups: []string{"*"}, Resources: []string{"*"},
				Operations: []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationList, v1beta1.ClusterResourceOperationGet},
				Selector:   &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: corev1.LabelMetadataName, Operator: metav1.LabelSelectorOpExists}}},
			}},
			ProxyOperations: []capsulerbac.ProxySettings{{Kind: capsulerbac.StorageClassesProxy, Operations: []capsulerbac.ProxyOperation{capsulerbac.ListOperation}}},
		}}},
	}
	objects := []client.Object{tenantA, setting,
		&capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tenant-b", Labels: map[string]string{corev1.LabelMetadataName: "tenant-b"}}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "foreign-role", Labels: map[string]string{corev1.LabelMetadataName: "foreign-role"}}},
	}
	for _, name := range []string{"tenant-a-ns", "tenant-b-ns", "kube-system"} {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{corev1.LabelMetadataName: name}}}
		if name != "kube-system" {
			ns.OwnerReferences = []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: name[:len(name)-3]}}
		}
		objects = append(objects, ns)
	}
	for i := range unrelated {
		name := fmt.Sprintf("unrelated-%d", i)
		objects = append(objects, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{corev1.LabelMetadataName: name}}})
	}
	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...)
	for _, idx := range []capsuleindexer.CustomIndexer{&indexer.TenantOwnerReference{}, &indexer.ProxySetting{}, &indexer.GlobalProxySetting{}, &tenantindexer.NamespacesReference{Obj: &capsulev1beta2.Tenant{}}} {
		builder = builder.WithIndex(idx.Object(), idx.Field(), idx.Func())
	}
	cl := &proxySettingCountingClient{Client: builder.Build()}
	gates := featuregate.NewFeatureGate()
	if err := gates.Add(map[featuregate.Feature]featuregate.FeatureSpec{features.ProxyClusterScoped: {Default: gate, PreRelease: featuregate.Alpha}}); err != nil {
		t.Fatal(err)
	}
	return proxySettingAccessFixture{filter: &kubeFilter{reader: cl, managerReader: cl, writer: cl, gates: gates, log: logr.Discard(), bearerToken: "proxy-token"}, client: cl, setting: setting}
}

func TestProxySettingCannotGrantClusterResources(t *testing.T) {
	t.Parallel()
	for _, gate := range []bool{false, true} {
		for _, identity := range []struct {
			kind capsulerbac.OwnerKind
			name string
		}{{capsulerbac.UserOwner, "alice"}, {capsulerbac.GroupOwner, "team"}, {capsulerbac.ServiceAccountOwner, "system:serviceaccount:tenant-a-ns:reader"}} {
			for _, owner := range []bool{false, true} {
				t.Run(fmt.Sprintf("gate=%t/%s/owner=%t", gate, identity.kind, owner), func(t *testing.T) {
					fixture := newProxySettingAccessFixture(t, gate, identity.kind, identity.name, owner, 0)
					username, groups := identity.name, []string(nil)
					if identity.kind == capsulerbac.GroupOwner {
						username, groups = "group-member", []string{identity.name}
					}
					proxyTenants, err := fixture.filter.getTenantsForOwner(t.Context(), username, groups)
					if err != nil {
						t.Fatal(err)
					}
					if len(proxyTenants) == 0 || proxyTenants[0].Tenant.Name != "tenant-a" {
						t.Fatal("tenant-scoped delegation was lost")
					}
					if !gate && !proxyTenants[0].RequestAllowed(httptest.NewRequest(http.MethodGet, "/apis/storage.k8s.io/v1/storageclasses", nil), capsulerbac.StorageClassesProxy) {
						t.Fatal("legacy tenant-scoped proxy operation was lost")
					}
					discovery := &discoveryfake.FakeDiscovery{Fake: &clienttesting.Fake{}}
					discovery.Resources = []*metav1.APIResourceList{{GroupVersion: rbacv1.SchemeGroupVersion.String(), APIResources: []metav1.APIResource{{Name: "clusterroles", Kind: "ClusterRole"}}}}
					type moduleCase struct {
						name, path, target string
						module             modules.Module
						named              bool
					}
					cases := []moduleCase{
						{"namespace list", "/api/v1/namespaces", "tenant-b-ns", namespacemodule.List(nil, fixture.client), false},
						{"namespace get", "/api/v1/namespaces/tenant-b-ns", "tenant-b-ns", namespacemodule.Get(nil, fixture.client), true},
						{"tenant list", "/apis/capsule.clastix.io/v1beta2/tenants", "tenant-b", tenants.List(fixture.client), false},
						{"tenant get", "/apis/capsule.clastix.io/v1beta2/tenants/tenant-b", "tenant-b", tenants.Get(fixture.client), true},
					}
					if gate {
						cases = append(cases,
							moduleCase{"cluster list", "/apis/rbac.authorization.k8s.io/v1/clusterroles", "foreign-role", clusterscoped.List(fixture.client, fixture.client, ""), false},
							moduleCase{"cluster get", "/apis/rbac.authorization.k8s.io/v1/clusterroles/foreign-role", "foreign-role", clusterscoped.Get(discovery, fixture.client, fixture.client, ""), true},
						)
					}
					for _, tc := range cases {
						t.Run(tc.name, func(t *testing.T) {
							httpRequest := mux.SetURLVars(httptest.NewRequest(http.MethodGet, tc.path, nil), map[string]string{"name": tc.target})
							selector, err := tc.module.Handle(proxyTenants, request.NewHTTP(httpRequest, nil, "", fixture.client, nil, nil, false, ""))
							if tc.named {
								if tc.name == "cluster get" {
									if err != nil || selector != nil {
										t.Fatalf("cluster GET must retain upstream RBAC: selector=%v error=%v", selector, err)
									}
									return
								}
								var denial moduleerrors.Error
								if !errors.As(err, &denial) || denial.Status().Code != http.StatusNotFound {
									t.Fatalf("namespaced ProxySetting granted privileged GET of %s", tc.target)
								}
								return
							}
							if err != nil || selector == nil {
								t.Fatalf("list selector = %v, error = %v", selector, err)
							}
							if selector.Matches(labels.Set{corev1.LabelMetadataName: tc.target}) {
								t.Errorf("namespaced ProxySetting exposes %s with selector %s", tc.target, selector)
							}
							if tc.name == "namespace list" {
								if fixture.client.namespaceLists != 0 {
									t.Errorf("ignored grant caused %d unnecessary namespace lists", fixture.client.namespaceLists)
								}
								if !selector.Matches(labels.Set{corev1.LabelMetadataName: "tenant-a-ns"}) || selector.Matches(labels.Set{corev1.LabelMetadataName: "kube-system"}) {
									t.Errorf("namespace delegation must stay inside tenant A: %s", selector)
								}
								httpRequest.URL.RawQuery = "labelSelector=kubernetes.io%2Fmetadata.name%3Dtenant-b-ns&limit=10"
								httpRequest.Header.Set("Authorization", "Bearer caller-token")
								httpRequest.Header.Set("Impersonate-User", "admin")
								httpRequest.Header.Set("Impersonate-Group", "system:masters")
								fixture.filter.handleRequest(httpRequest, selector, username)
								forwarded, parseErr := labels.Parse(httpRequest.URL.Query().Get("labelSelector"))
								if parseErr != nil || forwarded.Matches(labels.Set{corev1.LabelMetadataName: tc.target}) {
									t.Errorf("privileged forwarding loses isolation: selector=%v error=%v", forwarded, parseErr)
								}
								if httpRequest.Header.Get("Authorization") != "Bearer proxy-token" || httpRequest.Header.Get("Impersonate-User") != "" || httpRequest.Header.Get("Impersonate-Group") != "" {
									t.Error("filtered request did not use sanitized proxy credentials")
								}
								if httpRequest.URL.Query().Get("limit") != "10" || forwarded.Matches(labels.Set{corev1.LabelMetadataName: "tenant-a-ns"}) {
									t.Error("filtered forwarding lost the caller's query constraints")
								}
							}
						})
					}
					review := &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{Group: rbacv1.GroupName, Version: "v1", Resource: "clusterroles", Verb: "list"}}}
					var obj runtime.Object = review
					if err := authorization.MutateAuthorization(gate, proxyTenants, nil, &obj, schema.GroupVersionKind{Kind: "SelfSubjectAccessReview"}); err != nil {
						t.Fatal(err)
					}
					if review.Status.Allowed {
						t.Error("namespaced ProxySetting advertises unauthorized cluster access")
					}
				})
			}
		}
	}
}

func TestGlobalProxySettingsStillGrantClusterResources(t *testing.T) {
	t.Parallel()
	for _, gate := range []bool{false, true} {
		t.Run(fmt.Sprintf("gate=%t", gate), func(t *testing.T) {
			fixture := newProxySettingAccessFixture(t, gate, capsulerbac.UserOwner, "alice", false, 0)
			global := &v1beta1.GlobalProxySettings{ObjectMeta: metav1.ObjectMeta{Name: "admin-grant"}, Spec: v1beta1.GlobalProxySettingsSpec{Rules: []v1beta1.GlobalSubjectSpec{{
				Subjects:         []v1beta1.GlobalSubject{{Kind: capsulerbac.UserOwner, Name: "global-reader"}},
				ClusterResources: []v1beta1.ClusterResource{{APIGroups: []string{""}, Resources: []string{"namespaces"}, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: "tenant-b-ns"}}}},
			}}}}
			check := func(username string, allowed bool) {
				t.Helper()
				proxyTenants, err := fixture.filter.getTenantsForOwner(t.Context(), username, nil)
				if err != nil {
					t.Fatal(err)
				}
				req := request.NewHTTP(httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", nil), nil, "", fixture.client, nil, nil, false, "")
				selector, err := namespacemodule.List(nil, fixture.client).Handle(proxyTenants, req)
				if err != nil || selector == nil {
					t.Fatalf("list selector=%v error=%v", selector, err)
				}
				if selector.Matches(labels.Set{corev1.LabelMetadataName: "tenant-b-ns"}) != allowed || selector.Matches(labels.Set{corev1.LabelMetadataName: "kube-system"}) {
					t.Fatalf("incorrect admin grant for %s: %s", username, selector)
				}
			}
			check("global-reader", false)
			if err := fixture.client.Create(t.Context(), global); err != nil {
				t.Fatal(err)
			}
			check("global-reader", gate)
			check("alice", false)
			if err := fixture.client.Delete(t.Context(), global); err != nil {
				t.Fatal(err)
			}
			check("global-reader", false)
		})
	}
}

func TestProxySettingDelegationBoundaries(t *testing.T) {
	t.Parallel()
	for _, gate := range []bool{false, true} {
		t.Run(fmt.Sprintf("gate=%t", gate), func(t *testing.T) {
			fixture := newProxySettingAccessFixture(t, gate, capsulerbac.GroupOwner, "team", false, 0)
			check := func(username string, groups []string, allowed bool) {
				t.Helper()
				proxyTenants, err := fixture.filter.getTenantsForOwner(t.Context(), username, groups)
				if err != nil {
					t.Fatal(err)
				}
				req := request.NewHTTP(httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", nil), nil, "", fixture.client, nil, nil, false, "")
				selector, err := namespacemodule.List(nil, fixture.client).Handle(proxyTenants, req)
				if err != nil || selector == nil {
					t.Fatalf("list selector=%v error=%v", selector, err)
				}
				if selector.Matches(labels.Set{corev1.LabelMetadataName: "tenant-a-ns"}) != allowed || selector.Matches(labels.Set{corev1.LabelMetadataName: "tenant-b-ns"}) {
					t.Fatalf("incorrect tenant delegation for %s %v: %s", username, groups, selector)
				}
			}
			check("member", []string{"team"}, true)
			check("team", nil, false)
			check("member", []string{"other-team"}, false)
			if err := fixture.client.Delete(t.Context(), fixture.setting); err != nil {
				t.Fatal(err)
			}
			check("member", []string{"team"}, false)
			// A setting in a namespace with no Tenant association grants nothing.
			unbound := fixture.setting.DeepCopy()
			unbound.Namespace, unbound.ResourceVersion = "kube-system", ""
			if err := fixture.client.Create(t.Context(), unbound); err != nil {
				t.Fatal(err)
			}
			check("member", []string{"team"}, false)
		})
	}
}

func BenchmarkProxySettingNamespaceList(b *testing.B) {
	for _, gate := range []bool{false, true} {
		for _, unrelated := range []int{0, 100, 1000} {
			b.Run(fmt.Sprintf("gate=%t/unrelated=%d", gate, unrelated), func(b *testing.B) {
				fixture := newProxySettingAccessFixture(b, gate, capsulerbac.UserOwner, "alice", true, unrelated)
				mod := namespacemodule.List(nil, fixture.client)
				req := request.NewHTTP(httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", nil), nil, "", fixture.client, nil, nil, false, "")
				b.ReportAllocs()
				for b.Loop() {
					proxyTenants, err := fixture.filter.getTenantsForOwner(context.Background(), "alice", nil)
					if err != nil {
						b.Fatal(err)
					}
					selector, err := mod.Handle(proxyTenants, req)
					if err != nil || selector == nil || !selector.Matches(labels.Set{corev1.LabelMetadataName: "tenant-a-ns"}) {
						b.Fatalf("invalid list result: selector=%v error=%v", selector, err)
					}
				}
			})
		}
	}
}
