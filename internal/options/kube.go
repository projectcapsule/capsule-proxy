// Copyright 2020-2025 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package options

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/pkg/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"

	"github.com/projectcapsule/capsule-proxy/internal/request"
)

type kubeOpts struct {
	authTypes                  []request.AuthType
	url                        url.URL
	ignoredGroups              []string
	ignoredImpersonationGroups []string
	skipImpersonationReview    bool
	claimName                  string
	impersonationGroupsRegexp  *regexp.Regexp
	config                     *rest.Config
}

func NewKube(authTypes []request.AuthType, ignoredGroups []string, claimName string, config *rest.Config, ignoredImpersonationGroups []string, impersonationGroupsString string, skipImpersonationReview bool) (ListenerOpts, error) {
	u, err := url.Parse(config.Host)
	if err != nil {
		return nil, fmt.Errorf("cannot create Kubernetes Options due to failed URL parsing: %w", err)
	}

	var impersonationGroupsRegexp *regexp.Regexp
	if impersonationGroupsString != "" {
		impersonationGroupsRegexp, err = regexp.Compile(impersonationGroupsString)
		if err != nil {
			return nil, fmt.Errorf("cannot create Kubernetes Options due to failed regexp compilation: %w", err)
		}
	}

	return &kubeOpts{
		authTypes:                  authTypes,
		url:                        *u,
		ignoredGroups:              ignoredGroups,
		ignoredImpersonationGroups: ignoredImpersonationGroups,
		impersonationGroupsRegexp:  impersonationGroupsRegexp,
		skipImpersonationReview:    skipImpersonationReview,
		claimName:                  claimName,
		config:                     config,
	}, nil
}

func (k kubeOpts) AuthTypes() []request.AuthType {
	return k.authTypes
}

func (k kubeOpts) BearerToken() string {
	return k.config.BearerToken
}

func (k kubeOpts) BearerTokenFile() string {
	return k.config.BearerTokenFile
}

func (k kubeOpts) KubernetesControlPlaneURL() *url.URL {
	return &k.url
}

func (k kubeOpts) IgnoredGroupNames() []string {
	return k.ignoredGroups
}

func (k kubeOpts) IgnoredImpersonationsGroups() []string {
	return k.ignoredImpersonationGroups
}

func (k kubeOpts) ImpersonationGroupsRegexp() *regexp.Regexp {
	return k.impersonationGroupsRegexp
}

func (k kubeOpts) PreferredUsernameClaim() string {
	return k.claimName
}

func (k kubeOpts) SkipImpersonationReview() bool {
	return k.skipImpersonationReview
}

func (k kubeOpts) ReverseProxyTransport() (*http.Transport, error) {
	transportConfig, err := k.config.TransportConfig()
	if err != nil {
		return nil, errors.Wrap(err, "cannot get transport configuration")
	}

	tlsConfig, err := transport.TLSConfigFor(transportConfig)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create tls configuration")
	}

	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (conn net.Conn, e error) {
			return (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig:     tlsConfig,
	}, nil
}
