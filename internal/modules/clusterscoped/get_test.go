// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package clusterscoped

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	discoveryfake "k8s.io/client-go/discovery/fake"
	clienttesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
	proxyrequest "github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

const persistentVolumeName = "pvc-0574499c-f01b-4a53-85f1-ddb002de9cae"

func TestGetHandlesSelectedCoreNamedResource(t *testing.T) {
	t.Parallel()

	persistentVolume := &unstructured.Unstructured{}
	persistentVolume.SetAPIVersion("v1")
	persistentVolume.SetKind("PersistentVolume")
	persistentVolume.SetName(persistentVolumeName)
	persistentVolume.SetLabels(map[string]string{"capsule.clastix.io/tenant": "solar"})

	resourceClient := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).WithObjects(persistentVolume).Build()
	discoveryClient := &discoveryfake.FakeDiscovery{Fake: &clienttesting.Fake{}}
	discoveryClient.Resources = []*metav1.APIResourceList{{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{{Name: "persistentvolumes", Kind: "PersistentVolume"}},
	}}
	module := Get(discoveryClient, resourceClient, resourceClient, "/api/v1/persistentvolumes/{name}")

	httpRequest := httptest.NewRequest(http.MethodGet, "/api/v1/persistentvolumes/"+persistentVolumeName, nil)
	httpRequest = mux.SetURLVars(httpRequest, map[string]string{"name": persistentVolumeName})
	selector, err := module.Handle([]*tenant.ProxyTenant{{ClusterResources: []v1beta1.ClusterResource{{
		APIGroups:  []string{""},
		Resources:  []string{"persistentvolumes"},
		Operations: []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationGet},
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"capsule.clastix.io/tenant": "solar"},
		},
	}}}}, staticRequest{Request: httpRequest})
	if err != nil {
		t.Fatalf("unexpected GET handling error: %v", err)
	}
	if selector == nil || !selector.Matches(labels.Set(persistentVolume.GetLabels())) {
		t.Fatalf("selector %v does not select the persistent volume", selector)
	}
}

func TestGetHandlesLegacyListForSelectedGroupedNamedResource(t *testing.T) {
	t.Parallel()

	tenantOwner := &unstructured.Unstructured{}
	tenantOwner.SetAPIVersion("capsule.clastix.io/v1beta2")
	tenantOwner.SetKind("TenantOwner")
	tenantOwner.SetName("alice")
	tenantOwner.SetLabels(map[string]string{"projectcapsule.dev/tenant": "solar"})

	resourceClient := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).WithObjects(tenantOwner).Build()
	discoveryClient := &discoveryfake.FakeDiscovery{Fake: &clienttesting.Fake{}}
	discoveryClient.Resources = []*metav1.APIResourceList{{
		GroupVersion: "capsule.clastix.io/v1beta2",
		APIResources: []metav1.APIResource{{Name: "tenantowners", Kind: "TenantOwner"}},
	}}
	module := Get(discoveryClient, resourceClient, resourceClient, "/apis/capsule.clastix.io/v1beta2/tenantowners/{name}")

	httpRequest := httptest.NewRequest(http.MethodGet, "/apis/capsule.clastix.io/v1beta2/tenantowners/alice", nil)
	httpRequest = mux.SetURLVars(httpRequest, map[string]string{"name": "alice"})
	selector, err := module.Handle([]*tenant.ProxyTenant{{ClusterResources: []v1beta1.ClusterResource{{
		APIGroups:  []string{"capsule.clastix.io"},
		Resources:  []string{"tenantowners"},
		Operations: []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationList},
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"projectcapsule.dev/tenant": "solar"},
		},
	}}}}, staticRequest{Request: httpRequest})
	if err != nil {
		t.Fatalf("unexpected GET handling error: %v", err)
	}
	if selector == nil || !selector.Matches(labels.Set(tenantOwner.GetLabels())) {
		t.Fatalf("selector %v does not select the tenant owner", selector)
	}
}

type staticRequest struct {
	*http.Request
}

var _ proxyrequest.Request = staticRequest{}

func (r staticRequest) GetUserAndGroups() (string, []string, error) {
	return "alice", nil, nil
}

func (r staticRequest) GetHTTPRequest() *http.Request {
	return r.Request
}
