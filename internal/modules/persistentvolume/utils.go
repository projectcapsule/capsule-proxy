// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package persistentvolume

import (
	"net/http"

	capsuleapi "github.com/projectcapsule/capsule/pkg/api"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

func getPersistentVolume(req *http.Request, proxyTenants []*tenant.ProxyTenant, label string) (allowed bool, requirements labels.Requirement) {
	var tenantNames []string

	for _, pt := range proxyTenants {
		if ok := pt.RequestAllowed(req, capsuleapi.PersistentVolumesProxy); ok {
			allowed = true

			tenantNames = append(tenantNames, pt.Tenant.Name)
		}
	}

	requirement, _ := labels.NewRequirement(label, selection.In, tenantNames)

	return allowed, *requirement
}
