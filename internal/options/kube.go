package options

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/pkg/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
)

type kubeOpts struct {
	url        url.URL
	groupNames []string
	claimName  string
	config     *rest.Config
}

func NewKube(groupNames []string, claimName string, config *rest.Config) (ListenerOpts, error) {
	u, err := url.Parse(config.Host)
	if err != nil {
		return nil, fmt.Errorf("cannot create Kubernetes Options due to failed URL parsing: %w", err)
	}

	return &kubeOpts{
		url:        *u,
		groupNames: groupNames,
		claimName:  claimName,
		config:     config,
	}, nil
}

func (k kubeOpts) BearerToken() string {
	return k.config.BearerToken
}

func (k kubeOpts) KubernetesControlPlaneURL() *url.URL {
	return &k.url
}

func (k kubeOpts) UserGroupNames() []string {
	return k.groupNames
}

func (k kubeOpts) PreferredUsernameClaim() string {
	return k.claimName
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
