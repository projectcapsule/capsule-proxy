package webserver

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/cert"
)

type ListenerOptions interface {
	KubernetesControlPlaneUrl() *url.URL
	UserGroupName() string
	PreferredUsernameClaim() string
	ReverseProxyTransport() *http.Transport
	BearerToken() string
}

type kubeOptions struct {
	url       url.URL
	groupName string
	claimName string
	config    *rest.Config
}

func (k kubeOptions) BearerToken() string {
	return k.config.BearerToken
}

func NewKubeOptions(controlPlaneUrl string, groupName string, claimName string, config *rest.Config) (ListenerOptions, error) {
	u, err := url.Parse(controlPlaneUrl)
	if err != nil {
		return nil, fmt.Errorf("cannot create kubeOptions due to failed URL parsing: %s", err.Error())
	}

	return &kubeOptions{
		url:       *u,
		groupName: groupName,
		claimName: claimName,
		config:    config,
	}, nil
}

func (k kubeOptions) KubernetesControlPlaneUrl() *url.URL {
	return &k.url
}

func (k kubeOptions) UserGroupName() string {
	return k.groupName
}

func (k kubeOptions) PreferredUsernameClaim() string {
	return k.claimName
}

func (k kubeOptions) ReverseProxyTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (conn net.Conn, e error) {
			return (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig: func() *tls.Config {
			cp, _ := cert.NewPoolFromBytes(k.config.CAData)
			return &tls.Config{
				InsecureSkipVerify: true,
				Certificates: append([]tls.Certificate{}, tls.Certificate{
					Certificate: append([][]byte{}, k.config.CertData),
					PrivateKey:  append([][]byte{}, k.config.KeyData),
				}),
				RootCAs:    cp,
				NextProtos: k.config.NextProtos,
				ServerName: k.config.ServerName,
			}
		}(),
	}
}

type ServerOptions interface {
	IsListeningTls() bool
	ListeningPort() uint
	TlsCertificatePath() string
	TlsCertificateKeyPath() string
}

type httpOptions struct {
	isTls   bool
	port    uint
	crtPath string
	keyPath string
}

func NewServerOptions(isTls bool, listeningPort uint, certificatePath string, keyPath string) (ServerOptions, error) {
	var err error

	if isTls {
		_, err = os.Stat(certificatePath)
		if err != nil {
			return nil, fmt.Errorf("cannot lookup TLS certificate file: %s", err.Error())
		}
		_, err = os.Stat(keyPath)
		if err != nil {
			return nil, fmt.Errorf("cannot lookup TLS certificate key file: %s", err.Error())
		}
	}

	return &httpOptions{isTls: isTls, port: listeningPort, crtPath: certificatePath, keyPath: keyPath}, nil
}

func (h httpOptions) IsListeningTls() bool {
	return h.isTls
}

func (h httpOptions) ListeningPort() uint {
	return h.port
}

func (h httpOptions) TlsCertificatePath() string {
	return h.crtPath
}

func (h httpOptions) TlsCertificateKeyPath() string {
	return h.keyPath
}
