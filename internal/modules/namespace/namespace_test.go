// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gorilla/mux"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

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
		proxyTenants: []*tenant.ProxyTenant{tenantOwner("solar", "solar-dev", "fire-dev")},
		visible:      []string{"solar-dev", "fire-dev"},
	},
	{
		name:         "global rule",
		proxyTenants: []*tenant.ProxyTenant{globalRule(map[string]string{"env": "shared"})},
		visible:      []string{"fire-dev", "earth-dev"},
	},
	{
		name:         "tenant owner + global rule",
		proxyTenants: []*tenant.ProxyTenant{tenantOwner("solar", "solar-dev", "fire-dev"), globalRule(map[string]string{"env": "shared"})},
		visible:      []string{"solar-dev", "fire-dev", "earth-dev"},
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
		Subjects: []v1beta1.GlobalSubject{{Kind: v1beta1.GlobalSubjectKindUser, Name: "alice"}},
		ClusterResources: []v1beta1.ClusterResource{{
			APIGroups: []string{""},
			Resources: []string{"namespaces"},
			Selector:  &metav1.LabelSelector{MatchLabels: selector},
		}},
	}}, nil)
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

func TestGetNamespaceReadErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		err     error
		forward bool
	}{
		{name: "missing namespace delegates to API server", err: apierrors.NewNotFound(corev1.Resource("namespaces"), "shared"), forward: true},
		{name: "reader failure rejects global grant", err: errors.New("namespace reader failed")},
		{name: "forbidden read rejects global grant", err: apierrors.NewForbidden(corev1.Resource("namespaces"), "shared", errors.New("read denied"))},
		{name: "cancellation rejects global grant", err: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			httpRequest := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/shared", nil).WithContext(ctx)
			httpRequest = mux.SetURLVars(httpRequest, map[string]string{"name": "shared"})

			reads := 0
			reader := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
				Get: func(got context.Context, _ client.WithWatch, key client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					reads++
					if got != httpRequest.Context() || key.Name != "shared" {
						t.Errorf("unexpected namespace read: context=%v, key=%v", got, key)
					}
					return tc.err
				},
			}).Build()
			selector, err := Get(nil, reader).Handle([]*tenant.ProxyTenant{globalRule(map[string]string{"env": "shared"})}, testRequest{httpRequest})
			if reads != 1 {
				t.Errorf("namespace reads = %d, want 1", reads)
			}
			if tc.forward {
				if err != nil || selector == nil || !selector.Empty() {
					t.Fatalf("missing namespace must forward: selector=%v, err=%v", selector, err)
				}
				return
			}
			if selector != nil || err == nil {
				t.Fatalf("read failure must reject: selector=%v, err=%v", selector, err)
			}
			statusError, ok := err.(interface{ Status() *metav1.Status })
			if !ok {
				t.Fatalf("expected Kubernetes module error, got %T", err)
			}
			status := statusError.Status()
			if status.Code != http.StatusBadRequest || status.Reason != metav1.StatusReasonBadRequest || status.Message != tc.err.Error() {
				t.Errorf("unexpected read-error status: %+v", status)
			}
		})
	}
}

func TestGetNamespaceMissingReviewDoesNotGrant(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).Build()
	mod := Get(nil, reader)
	httpRequest := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/absent", nil), map[string]string{"name": "absent"})
	request := testRequest{httpRequest}
	selector, err := mod.Handle(nil, request)
	if err != nil || selector == nil || !selector.Empty() {
		t.Fatalf("GET must preserve upstream forwarding: selector=%v, err=%v", selector, err)
	}
	selector, err = mod.HandleReview(nil, request)
	if selector != nil || err == nil {
		t.Fatalf("missing namespace is not an authorization grant: selector=%v, err=%v", selector, err)
	}
	status, ok := err.(interface{ Status() *metav1.Status })
	if !ok || status.Status().Code != http.StatusNotFound {
		t.Fatalf("expected the same denial as an inaccessible namespace, got %v", err)
	}
}
