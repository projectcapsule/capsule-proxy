// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"net/http"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleapi "github.com/projectcapsule/capsule/pkg/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
)

type ProxyTenant struct {
	Tenant           capsulev1beta2.Tenant
	ProxySetting     map[capsuleapi.ProxyServiceKind]*Operations
	ClusterResources []v1beta1.ClusterResource
}

func defaultProxySettings() map[capsuleapi.ProxyServiceKind]*Operations {
	return map[capsuleapi.ProxyServiceKind]*Operations{
		capsuleapi.NodesProxy:             defaultOperations(),
		capsuleapi.StorageClassesProxy:    defaultOperations(),
		capsuleapi.IngressClassesProxy:    defaultOperations(),
		capsuleapi.PriorityClassesProxy:   defaultOperations(),
		capsuleapi.RuntimeClassesProxy:    defaultOperations(),
		capsuleapi.PersistentVolumesProxy: defaultOperations(),
	}
}

func NewProxyTenant(
	tenant capsulev1beta2.Tenant,
	ownerName string,
	ownerKind capsuleapi.OwnerKind,
	owners []v1beta1.OwnerSpec,
	disableLegacyProxySettings bool,
) *ProxyTenant {
	var (
		tenantProxySettings    []capsuleapi.ProxySettings
		tenantClusterResources []v1beta1.ClusterResource
	)

	for _, owner := range owners {
		if owner.Name == ownerName && owner.Kind == ownerKind {
			tenantClusterResources = owner.ClusterResources

			if !disableLegacyProxySettings {
				//nolint:staticcheck
				tenantProxySettings = owner.ProxyOperations
			}
		}
	}

	pt := &ProxyTenant{
		Tenant:           tenant,
		ClusterResources: tenantClusterResources,
	}

	if !disableLegacyProxySettings {
		proxySettings := defaultProxySettings()

		for _, setting := range tenantProxySettings {
			for _, operation := range setting.Operations {
				proxySettings[setting.Kind].Allow(operation)
			}
		}

		pt.ProxySetting = proxySettings
	}

	return pt
}

// NewClusterProxy returns a ProxyTenant struct for GlobalProxySettings. These settings are currently not bound to a tenant and therefore
// an empty tenant is returned.
func NewClusterProxy(ownerName string, ownerKind capsuleapi.OwnerKind, owners []v1beta1.GlobalSubjectSpec) *ProxyTenant {
	var tenantClusterResources []v1beta1.ClusterResource

	for _, global := range owners {
		for _, subject := range global.Subjects {
			if subject.Name == ownerName && subject.Kind == ownerKind {
				tenantClusterResources = append(tenantClusterResources, global.ClusterResources...)
			}
		}
	}

	return &ProxyTenant{
		Tenant: capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name: "global",
			},
			Spec: capsulev1beta2.TenantSpec{},
		},
		ClusterResources: tenantClusterResources,
	}
}

func (p *ProxyTenant) RequestAllowed(request *http.Request, serviceKind capsuleapi.ProxyServiceKind) (ok bool) {
	return p.ProxySetting[serviceKind].IsAllowed(request)
}
