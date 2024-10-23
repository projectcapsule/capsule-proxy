// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"net/http"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
)

type ProxyTenant struct {
	Tenant           capsulev1beta2.Tenant
	ProxySetting     map[capsulev1beta2.ProxyServiceKind]*Operations
	ClusterResources []v1beta1.ClusterResource
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
	var (
		tenantProxySettings    []capsulev1beta2.ProxySettings
		tenantClusterResources []v1beta1.ClusterResource
	)

	for _, owner := range owners {
		if owner.Name == ownerName && owner.Kind == ownerKind {
			tenantProxySettings = owner.ProxyOperations
			tenantClusterResources = owner.ClusterResources
		}
	}

	proxySettings := defaultProxySettings()

	for _, setting := range tenantProxySettings {
		for _, operation := range setting.Operations {
			proxySettings[setting.Kind].Allow(operation)
		}
	}

	return &ProxyTenant{
		Tenant:           tenant,
		ProxySetting:     proxySettings,
		ClusterResources: tenantClusterResources,
	}
}

// This Function returns a ProxyTenant struct for GlobalProxySettings. These Settings are currently not bound to a tenant and therefor
// an empty tenant and empty ProxySettings are returned.
func NewClusterProxy(ownerName string, ownerKind capsulev1beta2.OwnerKind, owners []v1beta1.GlobalSubjectSpec) *ProxyTenant {
	var tenantClusterResources []v1beta1.ClusterResource

	for _, global := range owners {
		for _, subject := range global.Subjects {
			if subject.Name == ownerName && subject.Kind == ownerKind {
				tenantClusterResources = global.ClusterResources
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
		ProxySetting:     defaultProxySettings(),
		ClusterResources: tenantClusterResources,
	}
}

func (p *ProxyTenant) RequestAllowed(request *http.Request, serviceKind capsulev1beta2.ProxyServiceKind) (ok bool) {
	return p.ProxySetting[serviceKind].IsAllowed(request)
}
