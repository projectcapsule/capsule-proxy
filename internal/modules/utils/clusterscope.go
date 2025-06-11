// Copyright 2020-2025 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"regexp"
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

func matchResource(gvk *schema.GroupVersionKind, cr v1beta1.ClusterResource) (match bool) {
	kindMatch := false
	groupVersionMatch := false

	for _, resource := range cr.Resources {
		if resource == "*" {
			kindMatch = true

			break
		}

		if gvk.Kind == resource {
			kindMatch = true

			break
		}
	}

	if !kindMatch {
		return match
	}

	// Check if the group/version matches any of the apiGroups using regex
	for _, apiGroup := range cr.APIGroups {
		// Handle wildcard "*" to match any group
		if apiGroup == "*" {
			groupVersionMatch = true

			break
		}

		// Replace "*" with ".*" for regex compatibility and ensure match against the entire string
		regexPattern := "^" + regexp.QuoteMeta(apiGroup) + "$"
		regexPattern = strings.ReplaceAll(regexPattern, "\\*", ".*")

		matched, _ := regexp.MatchString(regexPattern, gvk.Group+"/"+gvk.Version)
		if matched {
			groupVersionMatch = true

			break
		}
	}

	if kindMatch && groupVersionMatch {
		match = true
	}

	return match
}
