// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenants

import (
	"context"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/modules/clusterscoped"
	moduleerrors "github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
	"github.com/projectcapsule/capsule-proxy/internal/types"
)

func tenantsGVK() *schema.GroupVersionKind {
	return &schema.GroupVersionKind{
		Group:   types.CapsuleGroup,
		Version: capsulev1beta2.GroupVersion.Version,
		Kind:    types.Tenants,
	}
}

func clusterScopedTenantNames(ctx context.Context, reader client.Reader, proxyTenants []*tenant.ProxyTenant) ([]string, error) {
	selectors := clusterscoped.GetClusterScopeSelectors(
		tenantsGVK(),
		v1beta1.ClusterResourceOperationList,
		proxyTenants,
	)
	if len(selectors) == 0 {
		return nil, nil
	}

	selector, err := clusterscoped.UnionSelectors(selectors)
	if err != nil {
		return nil, moduleerrors.NewBadRequest(err, tenantsGVK().GroupKind())
	}

	tenantList := &capsulev1beta2.TenantList{}
	if err := reader.List(ctx, tenantList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(tenantList.Items))
	for i := range tenantList.Items {
		names = append(names, tenantList.Items[i].Name)
	}

	return names, nil
}

func matchesClusterScopedTenant(proxyTenants []*tenant.ProxyTenant, obj *capsulev1beta2.Tenant) bool {
	selectors := clusterscoped.GetClusterScopeSelectors(
		tenantsGVK(),
		v1beta1.ClusterResourceOperationGet,
		proxyTenants,
	)
	tenantLabels := labels.Set(obj.GetLabels())

	for _, selector := range selectors {
		if selector.Matches(tenantLabels) {
			return true
		}
	}

	return false
}
