package options

import (
	"crypto/x509"
)

type ServerOptions interface {
	IsListeningTLS() bool
	ListeningPort() uint
	TLSCertificatePath() string
	TLSCertificateKeyPath() string
	GetCertificateAuthorityPool() *x509.CertPool
}
