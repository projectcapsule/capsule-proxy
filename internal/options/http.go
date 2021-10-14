// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package options

import (
	"crypto/x509"
	"fmt"
	"os"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/cert"
)

type httpOptions struct {
	port    uint
	crtPath string
	keyPath string
	caPool  *x509.CertPool
}

func NewServer(port uint, crtPath string, keyPath string, config *rest.Config) (ServerOptions, error) {
	var err error

	if _, err = os.Stat(crtPath); err != nil {
		return nil, fmt.Errorf("cannot lookup TLS certificate file: %w", err)
	}

	if _, err = os.Stat(keyPath); err != nil {
		return nil, fmt.Errorf("cannot lookup TLS certificate key file: %w", err)
	}

	var caPool *x509.CertPool

	if caPool, err = cert.NewPool(config.CAFile); err != nil {
		if caPool, err = cert.NewPoolFromBytes(config.CAData); err != nil {
			return nil, fmt.Errorf("cannot find any CA data, nor from file nor from kubeconfig: %w", err)
		}
	}

	return &httpOptions{port: port, crtPath: crtPath, keyPath: keyPath, caPool: caPool}, nil
}

func (h httpOptions) GetCertificateAuthorityPool() *x509.CertPool {
	return h.caPool
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
