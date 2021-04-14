package options

import (
	"crypto/x509"
	"fmt"
	"os"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/cert"
)

type httpOptions struct {
	isTLS   bool
	port    uint
	crtPath string
	keyPath string
	caPool  *x509.CertPool
}

func NewServer(isTLS bool, port uint, crtPath string, keyPath string, config *rest.Config) (ServerOptions, error) {
	var err error

	if isTLS {
		if _, err = os.Stat(crtPath); err != nil {
			return nil, fmt.Errorf("cannot lookup TLS certificate file: %w", err)
		}

		if _, err = os.Stat(keyPath); err != nil {
			return nil, fmt.Errorf("cannot lookup TLS certificate key file: %w", err)
		}
	}

	var caPool *x509.CertPool

	if caPool, err = cert.NewPool(config.CAFile); err != nil {
		if caPool, err = cert.NewPoolFromBytes(config.CAData); err != nil {
			return nil, fmt.Errorf("cannot find any CA data, nor from file nor from kubeconfig: %w", err)
		}
	}

	return &httpOptions{isTLS: isTLS, port: port, crtPath: crtPath, keyPath: keyPath, caPool: caPool}, nil
}

func (h httpOptions) GetCertificateAuthorityPool() *x509.CertPool {
	return h.caPool
}

func (h httpOptions) IsListeningTLS() bool {
	return h.isTLS
}

func (h httpOptions) ListeningPort() uint {
	return h.port
}

func (h httpOptions) TLSCertificatePath() string {
	return h.crtPath
}

func (h httpOptions) TLSCertificateKeyPath() string {
	return h.keyPath
}
