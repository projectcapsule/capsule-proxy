package options

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
)

type kubeOpts struct {
	url       url.URL
	groupName string
	claimName string
	config    *rest.Config
}

func NewKube(controlPlaneUrl string, groupName string, claimName string, config *rest.Config) (ListenerOpts, error) {
	host := config.Host
	if controlPlaneUrl != "" {
		host = controlPlaneUrl
	}
	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("cannot create Kubernetes Options due to failed URL parsing: %s", err.Error())
	}

	return &kubeOpts{
		url:       *u,
		groupName: groupName,
		claimName: claimName,
		config:    config,
	}, nil
}

func (k kubeOpts) BearerToken() string {
	return k.config.BearerToken
}

func (k kubeOpts) KubernetesControlPlaneUrl() *url.URL {
	return &k.url
}

func (k kubeOpts) UserGroupName() string {
	return k.groupName
}

func (k kubeOpts) PreferredUsernameClaim() string {
	return k.claimName
}

func (k kubeOpts) ReverseProxyTransport() (*http.Transport, error) {
	transportConfig, err := k.config.TransportConfig()
	if err != nil {
		return nil, err
	}
	tlsConfig, err := transport.TLSConfigFor(transportConfig)
	if err != nil {
		return nil, err
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
