// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package clusterscoped

import (
	"errors"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"

	v1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

// GetClusterScopeSelectors preserves each rule's complete conjunction for a given
// GroupVersionKind and operation based on the ProxyTenants clusterResource
// configurations. Filtering per rule ensures a GET-only selector does not
// affect LIST. Legacy LIST rules include GET for v1beta1 compatibility.
func GetClusterScopeSelectors(
	gvk *schema.GroupVersionKind,
	operation v1beta1.ClusterResourceOperation,
	proxyTenants []*tenant.ProxyTenant,
) (selectors []labels.Selector) {
	for _, pt := range proxyTenants {
		for _, cr := range pt.ClusterResources {
			if !matchResource(gvk, cr) || !cr.AllowsOperation(operation) {
				continue
			}

			selector, err := metav1.LabelSelectorAsSelector(cr.Selector)
			if err != nil {
				continue
			}

			reqs, selectable := selector.Requirements()
			if !selectable || len(reqs) == 0 {
				continue
			}

			selectors = append(selectors, selector)
		}
	}

	return selectors
}

// UnionSelectors expresses OR between complete rules as a single upstream
// selector. Kubernetes only supports AND, so we merge rules with identical
// constraints except for one positive set requirement. Other combinations fail
// closed rather than introducing cross-combinations or buffering API responses.
func UnionSelectors(selectors []labels.Selector) (labels.Selector, error) {
	if len(selectors) == 0 {
		return nil, nil
	}

	if len(selectors) == 1 {
		return selectors[0], nil
	}

	merged, _ := selectors[0].Requirements()
	different := -1

	var values sets.Set[string]

	for _, selector := range selectors[1:] {
		next, _ := selector.Requirements()
		if len(merged) != len(next) {
			return nil, errors.New("cluster-resource selector union cannot be forwarded safely as one Kubernetes label selector")
		}

		for i := range merged {
			if merged[i].Equal(next[i]) {
				continue
			}

			if (different != -1 && different != i) || merged[i].Key() != next[i].Key() || !positiveSet(merged[i]) || !positiveSet(next[i]) {
				return nil, errors.New("cluster-resource selector union cannot be forwarded safely as one Kubernetes label selector")
			}

			if different == -1 {
				different = i
				values = sets.New(merged[i].ValuesUnsorted()...)
			}

			values.Insert(next[i].ValuesUnsorted()...)
		}
	}

	if different == -1 {
		return selectors[0], nil
	}

	requirement, err := labels.NewRequirement(merged[different].Key(), selection.In, values.UnsortedList())
	if err != nil {
		return nil, err
	}

	merged = append(labels.Requirements(nil), merged...)
	merged[different] = *requirement

	return labels.NewSelector().Add(merged...), nil
}

func positiveSet(requirement labels.Requirement) bool {
	switch requirement.Operator() { //nolint:exhaustive // Only positive membership operators can be merged by unioning values.
	case selection.In, selection.Equals, selection.DoubleEquals:
		return true
	default:
		return false
	}
}

// HasClusterScopeAccess includes the LIST representability check so self-reviews
// do not advertise combinations which the forwarding modules must reject.
func HasClusterScopeAccess(gvk *schema.GroupVersionKind, operation v1beta1.ClusterResourceOperation, proxyTenants []*tenant.ProxyTenant) bool {
	selectors := GetClusterScopeSelectors(gvk, operation, proxyTenants)
	if len(selectors) == 0 {
		return false
	}

	if operation == v1beta1.ClusterResourceOperationList {
		_, err := UnionSelectors(selectors)

		return err == nil
	}

	return true
}

func matchResource(gvk *schema.GroupVersionKind, cr v1beta1.ClusterResource) bool {
	if gvk == nil {
		return false
	}

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
