// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"net/http"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"

	"github.com/clastix/capsule-proxy/api/v1beta1"
)

type ProxyTenant struct {
	Tenant       capsulev1beta2.Tenant
	ProxySetting map[capsulev1beta2.ProxyServiceKind]*Operations
}

func defaultProxySettings() map[capsulev1beta2.ProxyServiceKind]*Operations {
	return map[capsulev1beta2.ProxyServiceKind]*Operations{
		capsulev1beta2.NodesProxy:             defaultOperations(),
		capsulev1beta2.StorageClassesProxy:    defaultOperations(),
		capsulev1beta2.IngressClassesProxy:    defaultOperations(),
		capsulev1beta2.PriorityClassesProxy:   defaultOperations(),
		capsulev1beta2.RuntimeClassesProxy:    defaultOperations(),
		capsulev1beta2.PersistentVolumesProxy: defaultOperations(),
	}
}

func NewProxyTenant(ownerName string, ownerKind capsulev1beta2.OwnerKind, tenant capsulev1beta2.Tenant, owners []v1beta1.OwnerSpec) *ProxyTenant {
	var tenantProxySettings []capsulev1beta2.ProxySettings

	for _, owner := range owners {
		if owner.Name == ownerName && owner.Kind == ownerKind {
			tenantProxySettings = owner.ProxyOperations
		}
	}

	proxySettings := defaultProxySettings()

	for _, setting := range tenantProxySettings {
		for _, operation := range setting.Operations {
			proxySettings[setting.Kind].Allow(operation)
		}
	}

	return &ProxyTenant{
		Tenant:       tenant,
		ProxySetting: proxySettings,
	}
}

func (p *ProxyTenant) RequestAllowed(request *http.Request, serviceKind capsulev1beta2.ProxyServiceKind) (ok bool) {
	return p.ProxySetting[serviceKind].IsAllowed(request)
}
