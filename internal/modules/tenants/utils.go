// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenants

import (
	"context"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/modules/clusterscoped"
	"github.com/projectcapsule/capsule-proxy/internal/modules/utils"
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
	_, requirements := clusterscoped.GetClusterScopeRequirements(tenantsGVK(), proxyTenants)
	if len(requirements) == 0 {
		return nil, nil
	}

	selector, err := utils.HandleListSelector(requirements)
	if err != nil {
		return nil, err
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
	_, requirements := clusterscoped.GetClusterScopeRequirements(tenantsGVK(), proxyTenants)
	tenantLabels := labels.Set(obj.GetLabels())

	for _, requirement := range requirements {
		if requirement.Matches(tenantLabels) {
			return true
		}
	}

	return false
}
