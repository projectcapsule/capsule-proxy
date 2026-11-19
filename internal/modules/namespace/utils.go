// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/modules/clusterscoped"
	moduleerrors "github.com/projectcapsule/capsule-proxy/internal/modules/errors"
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
// subjects granted access via GlobalProxySettings can list the
// corresponding namespaces without being tenant owners.
func clusterScopedNamespaceNames(ctx context.Context, reader client.Reader, proxyTenants []*tenant.ProxyTenant) ([]string, error) {
	selectors := clusterscoped.GetClusterScopeSelectors(
		namespacesGVK(),
		v1beta1.ClusterResourceOperationList,
		proxyTenants,
	)
	if len(selectors) == 0 {
		return nil, nil
	}

	selector, err := clusterscoped.UnionSelectors(selectors)
	if err != nil {
		return nil, moduleerrors.NewBadRequest(err, namespacesGVK().GroupKind())
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
// cluster-scoped ClusterResources rule from GlobalProxySettings,
// allowing subjects granted access through those rules to get
// the namespace without being tenant owners.
func matchesClusterScopedNamespace(proxyTenants []*tenant.ProxyTenant, ns *corev1.Namespace) bool {
	selectors := clusterscoped.GetClusterScopeSelectors(
		namespacesGVK(),
		v1beta1.ClusterResourceOperationGet,
		proxyTenants,
	)

	nsLabels := labels.Set(ns.GetLabels())
	for _, selector := range selectors {
		if selector.Matches(nsLabels) {
			return true
		}
	}

	return false
}
