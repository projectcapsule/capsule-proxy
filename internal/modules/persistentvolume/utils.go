// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package persistentvolume

import (
	"net/http"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	"github.com/clastix/capsule-proxy/internal/tenant"
)

func getPersistentVolume(req *http.Request, proxyTenants []*tenant.ProxyTenant, label string) (allowed bool, requirements labels.Requirement) {
	var tenantNames []string

	for _, pt := range proxyTenants {
		if ok := pt.RequestAllowed(req, capsulev1beta2.PersistentVolumesProxy); ok {
			allowed = true

			tenantNames = append(tenantNames, pt.Tenant.Name)
		}
	}

	requirement, _ := labels.NewRequirement(label, selection.In, tenantNames)

	return allowed, *requirement
}
