// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

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
