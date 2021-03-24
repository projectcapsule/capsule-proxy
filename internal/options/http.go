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

func NewServer(isTLS bool, listeningPort uint, certificatePath string, keyPath string, config *rest.Config) (ServerOptions, error) {
	var err error

	if isTLS {
		_, err = os.Stat(certificatePath)
		if err != nil {
			return nil, fmt.Errorf("cannot lookup TLS certificate file: %s", err.Error())
		}
		_, err = os.Stat(keyPath)
		if err != nil {
			return nil, fmt.Errorf("cannot lookup TLS certificate key file: %s", err.Error())
		}
	}

	var caPool *x509.CertPool
	caPool, err = cert.NewPool(config.CAFile)
	if err != nil {
		caPool, err = cert.NewPoolFromBytes(config.CAData)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot find any CA data, nor from file nor from kubeconfig: %s", err.Error())
	}
	return &httpOptions{isTLS: isTLS, port: listeningPort, crtPath: certificatePath, keyPath: keyPath, caPool: caPool}, nil
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
