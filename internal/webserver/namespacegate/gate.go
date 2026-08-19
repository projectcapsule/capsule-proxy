// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

// Package namespacegate lets create-if-missing clients distinguish a missing
// namespace from a resource-level authorization denial.
package namespacegate

import (
	"net/http"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Gate mutates only forbidden responses for named resources in namespaces that
// the authoritative API confirms do not exist.
type Gate struct {
	namespaces client.Reader
	log        logr.Logger
}

func New(namespaces client.Reader, log logr.Logger) *Gate {
	return &Gate{
		namespaces: namespaces,
		log:        log,
	}
}

// ModifyResponse is intended for httputil.ReverseProxy.ModifyResponse.
// An inability to prove that the namespace is absent preserves the upstream
// response unchanged.
func (g *Gate) ModifyResponse(response *http.Response) error {
	if response == nil || response.Request == nil {
		return nil
	}

	if response.StatusCode == http.StatusForbidden {
		g.maskForbiddenForMissingNamespace(response)
	}

	return nil
}
