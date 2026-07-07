// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/modules/clusterscoped"
	"github.com/projectcapsule/capsule-proxy/internal/modules/utils"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
	"github.com/projectcapsule/capsule-proxy/internal/types"
)

// namespacesGVK returns the GroupVersionKind used to match cluster-scoped
// ClusterResources rules against the core namespaces resource.
func namespacesGVK() *schema.GroupVersionKind {
	return &schema.GroupVersionKind{Group: corev1.GroupName, Version: types.V1, Kind: types.Namespaces}
}

// clusterScopedNamespaceNames resolves the namespaces selected by cluster-scoped
// ClusterResources rules (matching the namespaces resource) to their names, so
// subjects granted access via GlobalProxySettings or ProxySettings can list the
// corresponding namespaces without being tenant owners.
func clusterScopedNamespaceNames(ctx context.Context, reader client.Reader, proxyTenants []*tenant.ProxyTenant) ([]string, error) {
	_, requirements := clusterscoped.GetClusterScopeRequirements(namespacesGVK(), proxyTenants)
	if len(requirements) == 0 {
		return nil, nil
	}

	selector, err := utils.HandleListSelector(requirements)
	if err != nil {
		return nil, err
	}

	nsList := &corev1.NamespaceList{}
	if err := reader.List(ctx, nsList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(nsList.Items))
	for i := range nsList.Items {
		names = append(names, nsList.Items[i].GetName())
	}

	return names, nil
}

// matchesClusterScopedNamespace reports whether the namespace is selected by any
// cluster-scoped ClusterResources rule (e.g. from GlobalProxySettings or
// ProxySettings), allowing subjects granted access through those rules to get
// the namespace without being tenant owners.
func matchesClusterScopedNamespace(proxyTenants []*tenant.ProxyTenant, ns *corev1.Namespace) bool {
	_, requirements := clusterscoped.GetClusterScopeRequirements(namespacesGVK(), proxyTenants)

	nsLabels := labels.Set(ns.GetLabels())
	for _, requirement := range requirements {
		if requirement.Matches(nsLabels) {
			return true
		}
	}

	return false
}
