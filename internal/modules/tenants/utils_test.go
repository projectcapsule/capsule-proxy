// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenants

import (
	"context"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	proxyv1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

func TestClusterScopedTenantNames(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "selected", Labels: map[string]string{"environment": "shared"}}},
		&capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "hidden", Labels: map[string]string{"environment": "private"}}},
	).Build()
	proxyTenants := []*tenant.ProxyTenant{{ClusterResources: []proxyv1beta1.ClusterResource{{
		APIGroups: []string{"capsule.clastix.io"},
		Resources: []string{"tenants"},
		Selector:  &metav1.LabelSelector{MatchLabels: map[string]string{"environment": "shared"}},
	}}}}

	names, err := clusterScopedTenantNames(context.Background(), reader, proxyTenants)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "selected" {
		t.Fatalf("expected selected tenant, got %v", names)
	}
}

func TestMatchesClusterScopedTenant(t *testing.T) {
	t.Parallel()

	proxyTenants := []*tenant.ProxyTenant{{ClusterResources: []proxyv1beta1.ClusterResource{{
		APIGroups: []string{"capsule.clastix.io/v1beta2"},
		Resources: []string{"tenants"},
		Selector:  &metav1.LabelSelector{MatchLabels: map[string]string{"access": "global"}},
	}}}}

	if !matchesClusterScopedTenant(proxyTenants, &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"access": "global"}},
	}) {
		t.Fatal("expected tenant to match the cluster-scoped rule")
	}
	if matchesClusterScopedTenant(proxyTenants, &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"access": "private"}},
	}) {
		t.Fatal("expected tenant not to match the cluster-scoped rule")
	}
}
