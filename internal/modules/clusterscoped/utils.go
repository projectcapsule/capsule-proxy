// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package clusterscoped

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

// Calculate Requirements for a given GroupVersionKind based on the ProxyTenants clusterResource configurations.
func GetClusterScopeRequirements(gvk *schema.GroupVersionKind, proxyTenants []*tenant.ProxyTenant) (operations []v1beta1.ClusterResourceOperation, requirements []labels.Requirement) {
	operations = []v1beta1.ClusterResourceOperation{}
	requirements = []labels.Requirement{}

	for _, pt := range proxyTenants {
		for _, cr := range pt.ClusterResources {
			if matchResource(gvk, cr) {
				// Append Operations
				operations = append(operations, cr.Operations...)

				// Append Selector
				selector, err := metav1.LabelSelectorAsSelector(cr.Selector)
				if err != nil {
					continue
				}

				reqs, selectable := selector.Requirements()
				if !selectable {
					continue
				}

				requirements = append(requirements, reqs...)
			}
		}
	}

	return operations, requirements
}

func matchResource(gvk *schema.GroupVersionKind, cr v1beta1.ClusterResource) bool {
	kindMatch := false

	for _, r := range cr.Resources {
		if r == "*" || r == gvk.Kind {
			kindMatch = true

			break
		}
	}

	if !kindMatch {
		return false
	}

	// --- Group / GroupVersion match ---
	for _, apiGroup := range cr.APIGroups {
		if apiGroup == "*" {
			return true
		}

		// Decide matching target
		target := gvk.Group
		if strings.Contains(apiGroup, "/") {
			target = gvk.Group + "/" + gvk.Version
		}

		if matchPattern(apiGroup, target) {
			return true
		}
	}

	return false
}

func matchPattern(pattern, value string) bool {
	if pattern == "*" {
		return true
	}

	// No wildcard → exact match
	if !strings.Contains(pattern, "*") {
		return pattern == value
	}

	parts := strings.Split(pattern, "*")

	// Single '*' at start or end prefix/suffix optimization
	if len(parts) == 2 {
		if parts[0] == "" {
			return strings.HasSuffix(value, parts[1])
		}

		if parts[1] == "" {
			return strings.HasPrefix(value, parts[0])
		}
	}

	idx := 0

	for _, part := range parts {
		if part == "" {
			continue
		}

		i := strings.Index(value[idx:], part)
		if i < 0 {
			return false
		}

		idx += i + len(part)
	}

	return true
}
