// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"net/http"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

type ProxyTenant struct {
	Tenant       capsulev1beta1.Tenant
	ProxySetting map[capsulev1beta1.ProxyServiceKind]*Operations
}

func defaultProxySettings() map[capsulev1beta1.ProxyServiceKind]*Operations {
	return map[capsulev1beta1.ProxyServiceKind]*Operations{
		capsulev1beta1.NodesProxy:           defaultOperations(),
		capsulev1beta1.StorageClassesProxy:  defaultOperations(),
		capsulev1beta1.IngressClassesProxy:  defaultOperations(),
		capsulev1beta1.PriorityClassesProxy: defaultOperations(),
	}
}

func NewProxyTenant(ownerName string, ownerKind capsulev1beta1.OwnerKind, tenant capsulev1beta1.Tenant, owners capsulev1beta1.OwnerListSpec) *ProxyTenant {
	var tenantProxySettings []capsulev1beta1.ProxySettings

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

func (p *ProxyTenant) RequestAllowed(request *http.Request, serviceKind capsulev1beta1.ProxyServiceKind) (ok bool) {
	return p.ProxySetting[serviceKind].IsAllowed(request)
}
