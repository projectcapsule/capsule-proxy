package options

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/cert"
)

type kubeOpts struct {
	url         url.URL
	groupName   string
	claimName   string
	bearerToken string
	config      *rest.Config
}

func NewKube(controlPlaneUrl string, groupName string, claimName string, config *rest.Config, bearerToken string) (ListenerOpts, error) {
	u, err := url.Parse(controlPlaneUrl)
	if err != nil {
		return nil, fmt.Errorf("cannot create Kubernetes Options due to failed URL parsing: %s", err.Error())
	}

	kubeOpts := &kubeOpts{
		url:         *u,
		groupName:   groupName,
		claimName:   claimName,
		bearerToken: config.BearerToken,
		config:      config,
	}

	if _, err := rest.InClusterConfig(); err != nil {
		kubeOpts.bearerToken = bearerToken
	}

	return kubeOpts, nil
}

func (k kubeOpts) BearerToken() string {
	fmt.Printf("token %v\n", k.bearerToken)
	return k.bearerToken
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

func (k kubeOpts) ReverseProxyTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (conn net.Conn, e error) {
			return (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig: &tls.Config{
			RootCAs: func() (cp *x509.CertPool) {
				var err error
				cp, err = cert.NewPool(k.config.CAFile)
				if err != nil {
					cp, _ = cert.NewPoolFromBytes(k.config.CAData)
				}
				return
			}(),
			NextProtos: k.config.NextProtos,
			ServerName: k.config.ServerName,
		},
	}
}
