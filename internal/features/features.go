// Copyright 2020-2021 Clastix Labs
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
)
