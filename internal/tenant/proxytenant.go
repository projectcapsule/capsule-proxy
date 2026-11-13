// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"net/http"
	"slices"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
)

type ProxyTenant struct {
	Tenant           capsulev1beta2.Tenant
	ProxySetting     map[capsulerbac.ProxyServiceKind]*Operations
	ClusterResources []v1beta1.ClusterResource
}

func defaultProxySettings() map[capsulerbac.ProxyServiceKind]*Operations {
	return map[capsulerbac.ProxyServiceKind]*Operations{
		capsulerbac.NodesProxy:             defaultOperations(),
		capsulerbac.StorageClassesProxy:    defaultOperations(),
		capsulerbac.IngressClassesProxy:    defaultOperations(),
		capsulerbac.PriorityClassesProxy:   defaultOperations(),
		capsulerbac.RuntimeClassesProxy:    defaultOperations(),
		capsulerbac.PersistentVolumesProxy: defaultOperations(),
	}
}

func NewProxyTenant(
	tenant capsulev1beta2.Tenant,
	ownerName string,
	ownerKind capsulerbac.OwnerKind,
	owners []v1beta1.OwnerSpec,
	disableLegacyProxySettings bool,
) *ProxyTenant {
	var tenantProxySettings []capsulerbac.ProxySettings

	for _, owner := range owners {
		if owner.Name == ownerName && owner.Kind == ownerKind {
			if !disableLegacyProxySettings {
				//nolint:staticcheck
				tenantProxySettings = owner.ProxyOperations
			}
		}
	}

	// Namespaced ProxySettings are tenant-writable and cannot grant cluster-wide
	// access. Ignore ClusterResources even on objects stored before CRD validation
	// was tightened. Only NewClusterProxy may import those administrator grants.
	pt := &ProxyTenant{Tenant: tenant}

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
//
// A rule applies when one of its subjects is the caller itself (ownerKind/ownerName) or a Tenant the caller
// currently owns (ownedTenants).
func NewClusterProxy(ownerName string, ownerKind capsulerbac.OwnerKind, owners []v1beta1.GlobalSubjectSpec, ownedTenants []string) *ProxyTenant {
	var tenantClusterResources []v1beta1.ClusterResource

	for _, global := range owners {
		for _, subject := range global.Subjects {
			if globalSubjectMatches(subject, ownerName, ownerKind, ownedTenants) {
				tenantClusterResources = append(tenantClusterResources, global.ClusterResources...)

				break
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

func globalSubjectMatches(subject v1beta1.GlobalSubject, ownerName string, ownerKind capsulerbac.OwnerKind, ownedTenants []string) bool {
	if subject.Kind == v1beta1.GlobalSubjectKindTenant {
		return slices.Contains(ownedTenants, subject.Name)
	}

	return subject.Name == ownerName && subject.Kind.String() == ownerKind.String()
}

func (p *ProxyTenant) RequestAllowed(request *http.Request, serviceKind capsulerbac.ProxyServiceKind) (ok bool) {
	return p.ProxySetting[serviceKind].IsAllowed(request)
}
