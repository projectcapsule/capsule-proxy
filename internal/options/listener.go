// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package options

import (
	"net/http"
	"net/url"
	"regexp"

	"github.com/projectcapsule/capsule-proxy/internal/request"
)

type ListenerOpts interface {
	AuthTypes() []request.AuthType
	KubernetesControlPlaneURL() *url.URL
	IgnoredGroupNames() []string
	IgnoredImpersonationsGroups() []string
	ImpersonationGroupsRegexp() *regexp.Regexp
	PreferredUsernameClaim() string
	ReverseProxyTransport() (*http.Transport, error)
	BearerToken() string
	SkipImpersonationReview() bool
}
