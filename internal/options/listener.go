// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package options

import (
	"net/http"
	"net/url"

	"github.com/clastix/capsule-proxy/internal/request"
)

type ListenerOpts interface {
	AuthTypes() []request.AuthType
	KubernetesControlPlaneURL() *url.URL
	IgnoredGroupNames() []string
	PreferredUsernameClaim() string
	ReverseProxyTransport() (*http.Transport, error)
	BearerToken() string
}
