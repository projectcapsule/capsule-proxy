// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenants

import (
	"net/http"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
	"github.com/projectcapsule/capsule-proxy/internal/types"
)

type list struct {
	reader client.Reader
	log    logr.Logger
	gk     schema.GroupVersionKind
}

func List(reader client.Reader) modules.Module {
	return &list{
		reader: reader,
		log:    ctrl.Log.WithName("tenant_list"),
		gk: schema.GroupVersionKind{
			Group:   types.CapsuleGroup,
			Version: "*",
			Kind:    types.Tenants,
		},
	}
}

func (l list) GroupVersionKind() schema.GroupVersionKind {
	return l.gk
}

func (l list) GroupKind() schema.GroupKind {
	return l.gk.GroupKind()
}

func (l list) Path() string {
	return basePath
}

func (l list) Methods() []string {
	return []string{http.MethodGet}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	userTenants := make([]string, 0, len(proxyTenants))

	for _, tnt := range proxyTenants {
		userTenants = append(userTenants, tnt.Tenant.Name)
	}

	clusterScoped, err := clusterScopedTenantNames(proxyRequest.GetHTTPRequest().Context(), l.reader, proxyTenants)
	if err != nil {
		return nil, errors.NewBadRequest(err, l.GroupKind())
	}

	userTenants = sets.List(sets.New(append(userTenants, clusterScoped...)...))

	var r *labels.Requirement

	switch {
	case len(userTenants) > 0:
		r, err = labels.NewRequirement("kubernetes.io/metadata.name", selection.In, userTenants)
	default:
		r, err = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	if err != nil {
		return nil, errors.NewBadRequest(err, l.GroupKind())
	}

	return labels.NewSelector().Add(*r), nil
}
