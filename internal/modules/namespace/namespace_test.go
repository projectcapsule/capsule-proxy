// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gorilla/mux"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

var namespaces = []*corev1.Namespace{
	newNamespace("solar-dev", "solar", nil),
	newNamespace("fire-dev", "solar", map[string]string{"env": "shared"}),
	newNamespace("wind-dev", "wind", nil),
	newNamespace("earth-dev", "", map[string]string{"env": "shared"}),
	newNamespace("other", "", nil), // included to ensure that the proxy doesn't fail open. none of the cases should include this
}

var namespaceCases = []struct {
	name         string
	proxyTenants []*tenant.ProxyTenant
	visible      []string
}{
	{
		name:    "no access",
		visible: nil,
	},
	{
		name:         "tenant owner",
		proxyTenants: []*tenant.ProxyTenant{tenantOwner("wind", "wind-dev")},
		visible:      []string{"wind-dev"},
	},
	{
		name:         "tenant owner multiple",
		proxyTenants: []*tenant.ProxyTenant{tenantOwner("solar", "solar-dev","fire-dev")},
		visible:      []string{"solar-dev", "fire-dev"},
	},
	{
		name:         "global rule",
		proxyTenants: []*tenant.ProxyTenant{globalRule(map[string]string{"env": "shared"})},
		visible:      []string{ "fire-dev", "earth-dev"},
	},
	{
		name:         "tenant owner + global rule",
		proxyTenants: []*tenant.ProxyTenant{tenantOwner("solar", "solar-dev","fire-dev"), globalRule(map[string]string{"env": "shared"})},
		visible:      []string{"solar-dev","fire-dev", "earth-dev"},
	},
}

func newNamespace(name, ownerTenant string, extraLabels map[string]string) *corev1.Namespace {
	ns := &corev1.Namespace{}
	ns.Name = name
	ns.Labels = map[string]string{corev1.LabelMetadataName: name}

	for k, v := range extraLabels {
		ns.Labels[k] = v
	}

	if ownerTenant != "" {
		ns.OwnerReferences = []metav1.OwnerReference{{Kind: "Tenant", Name: ownerTenant}}
	}

	return ns
}

func tenantOwner(name string, namespaces ...string) *tenant.ProxyTenant {
	pt := &tenant.ProxyTenant{}
	pt.Tenant = capsulev1beta2.Tenant{}
	pt.Tenant.Name = name
	pt.Tenant.Status.Namespaces = namespaces

	return pt
}

func globalRule(selector map[string]string) *tenant.ProxyTenant {
	return tenant.NewClusterProxy("alice", capsulerbac.UserOwner, []v1beta1.GlobalSubjectSpec{{
		Subjects: []v1beta1.GlobalSubject{{Kind: capsulerbac.UserOwner, Name: "alice"}},
		ClusterResources: []v1beta1.ClusterResource{{
			APIGroups: []string{""},
			Resources: []string{"namespaces"},
			Selector:  &metav1.LabelSelector{MatchLabels: selector},
		}},
	}})
}

type testRequest struct{ *http.Request }

func (r testRequest) GetUserAndGroups() (string, []string, error) { return "alice", nil, nil }

func (r testRequest) GetHTTPRequest() *http.Request { return r.Request }

// TestNamespaces checks that List returns exactly the namespaces in visible
// and that Get allows exactly that same set of namespaces.
func TestNamespaces(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, ns := range namespaces {
		builder = builder.WithObjects(ns.DeepCopy())
	}

	reader := builder.Build()

	for _, tc := range namespaceCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			request := testRequest{httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", nil)}

			selector, err := List(nil, reader).Handle(tc.proxyTenants, request)
			if err != nil {
				t.Fatalf("list: %v", err)
			}

			var listed []string

			for _, ns := range namespaces {
				if selector.Matches(labels.Set(ns.Labels)) {
					listed = append(listed, ns.Name)
				}
			}

			if !slices.Equal(listed, tc.visible) {
				t.Errorf("list: got %v, want %v", listed, tc.visible)
			}

			for _, ns := range namespaces {
				httpRequest := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/"+ns.Name, nil)
				httpRequest = mux.SetURLVars(httpRequest, map[string]string{"name": ns.Name})

				_, err := Get(nil, reader).Handle(tc.proxyTenants, testRequest{httpRequest})
				if allowed, want := err == nil, slices.Contains(tc.visible, ns.Name); allowed != want {
					t.Errorf("get %s: allowed=%v, want %v (err: %v)", ns.Name, allowed, want, err)
				}
			}
		})
	}
}
