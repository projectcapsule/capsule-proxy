// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"k8s.io/apimachinery/pkg/labels"

	"github.com/clastix/capsule-proxy/internal/request"
	"github.com/clastix/capsule-proxy/internal/tenant"
)

type Module interface {
	Path() string
	Methods() []string
	Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error)
}
