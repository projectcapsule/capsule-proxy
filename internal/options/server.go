package options

import (
	"crypto/x509"
)

type ServerOptions interface {
	IsListeningTls() bool
	ListeningPort() uint
	TlsCertificatePath() string
	TlsCertificateKeyPath() string
	GetCertificateAuthorityPool() *x509.CertPool
}
