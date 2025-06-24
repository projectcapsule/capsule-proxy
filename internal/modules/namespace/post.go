// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"net/http"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

type post struct{}

func Post() modules.Module {
	return &post{}
}

func (l post) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{}
}

func (l post) GroupKind() schema.GroupKind {
	return schema.GroupKind{}
}

func (l post) Path() string {
	return basePath
}

func (l post) Methods() []string {
	return []string{http.MethodPost, http.MethodPut, http.MethodPatch}
}

func (l post) Handle([]*tenant.ProxyTenant, request.Request) (selector labels.Selector, err error) {
	return nil, nil
}
