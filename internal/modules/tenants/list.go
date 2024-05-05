// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package tenants

import (
	"net/http"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

type list struct {
	log logr.Logger
	gk  schema.GroupKind
}

func List() modules.Module {
	return &list{
		log: ctrl.Log.WithName("tenant_list"),
		gk: schema.GroupKind{
			Group: "capsule.clastix.io",
			Kind:  "tenants",
		},
	}
}

func (l list) GroupKind() schema.GroupKind {
	return l.gk
}

func (l list) Path() string {
	return basePath
}

func (l list) Methods() []string {
	return []string{http.MethodGet}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, _ request.Request) (selector labels.Selector, err error) {
	userTenants := make([]string, 0, len(proxyTenants))

	for _, tnt := range proxyTenants {
		userTenants = append(userTenants, tnt.Tenant.Name)
	}

	var r *labels.Requirement

	switch {
	case len(userTenants) > 0:
		r, err = labels.NewRequirement("kubernetes.io/metadata.name", selection.In, userTenants)
	default:
		r, err = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	if err != nil {
		return nil, errors.NewBadRequest(err, l.gk)
	}

	return labels.NewSelector().Add(*r), nil
}
