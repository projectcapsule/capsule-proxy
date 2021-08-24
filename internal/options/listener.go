// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package options

import (
	"net/http"
	"net/url"
)

type ListenerOpts interface {
	KubernetesControlPlaneURL() *url.URL
	UserGroupNames() []string
	PreferredUsernameClaim() string
	ReverseProxyTransport() (*http.Transport, error)
	BearerToken() string
}
