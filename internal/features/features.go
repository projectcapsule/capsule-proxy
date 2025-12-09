// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package features

const (
	// ProxyAllNamespaced allows to proxy all the Namespaced objects
	// for all tenant users
	//
	// When enabled, it will discover apis and ensure labels are set
	// for resources in all tenant namespaces resulting in increased memory
	// usage and cluster-wide RBAC permissions (list and watch).
	ProxyAllNamespaced = "ProxyAllNamespaced"

	// SkipImpersonationReview allows to skip the impersonation review
	// for all requests containing impersonation headers (user and groups)
	//
	// DANGER: Enabling this flag allows any user to impersonate as any user or group
	// essentially bypassing any authorization. Only use this option in trusted environments
	// where authorization/authentication is offloaded to external systems.
	SkipImpersonationReview = "SkipImpersonationReview"

	// ProxyClusterScoped allows to proxy all clusterScoped objects
	// for all tenant users.
	ProxyClusterScoped = "ProxyClusterScoped"
)
