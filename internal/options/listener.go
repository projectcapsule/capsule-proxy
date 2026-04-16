// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package options

import (
	"net"
	"net/http"
	"net/url"
	"regexp"

	"github.com/projectcapsule/capsule-proxy/internal/request"
)

//nolint:interfacebloat
type ListenerOpts interface {
	AuthTypes() []request.AuthType
	KubernetesControlPlaneURL() *url.URL
	IgnoredGroupNames() []string
	IgnoredImpersonationsGroups() []string
	ImpersonationGroupsRegexp() *regexp.Regexp
	PreferredUsernameClaim() string
	ReverseProxyTransport() (*http.Transport, error)
	BearerTokenFile() string
	BearerToken() string
	SkipImpersonationReview() bool
	TrustedProxyCIDRs() []*net.IPNet
	XFCCHeader() string
	AllowedPaths() []string
}
