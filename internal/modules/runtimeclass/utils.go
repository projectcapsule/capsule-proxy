// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package runtimeclass

import (
	"net/http"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/clastix/capsule-proxy/internal/tenant"
)

func getRuntimeClass(req *http.Request, proxyTenants []*tenant.ProxyTenant) (allowed bool, requirements []labels.Requirement) {
	requirements = []labels.Requirement{}

	for _, pt := range proxyTenants {
		if ok := pt.RequestAllowed(req, capsulev1beta2.RuntimeClassesProxy); ok {
			allowed = true

			rc := pt.Tenant.Spec.RuntimeClasses
			if rc == nil {
				continue
			}

			selector, err := metav1.LabelSelectorAsSelector(&rc.LabelSelector)
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

	return allowed, requirements
}
