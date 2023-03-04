// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"net/http"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	"github.com/clastix/capsule-proxy/internal/tenant"
)

func GetNodeSelector(nl *corev1.NodeList, selectors []map[string]string) (*labels.Requirement, error) {
	var names []string

	for _, node := range nl.Items {
		for _, selector := range selectors {
			matches := 0

			for k := range selector {
				if selector[k] == node.GetLabels()[k] {
					matches++
				}
			}

			if matches == len(selector) {
				names = append(names, node.GetName())
			}
		}
	}

	if len(names) > 0 {
		return labels.NewRequirement("kubernetes.io/hostname", selection.In, names)
	}

	return nil, fmt.Errorf("cannot create LabelSelector for the requested Node requirement")
}

func GetNodeSelectors(request *http.Request, proxyTenants []*tenant.ProxyTenant) (selectors []map[string]string) {
	for _, pt := range proxyTenants {
		if ok := pt.RequestAllowed(request, capsulev1beta2.NodesProxy); ok {
			selectors = append(selectors, pt.Tenant.Spec.NodeSelector)
		}
	}

	return
}
