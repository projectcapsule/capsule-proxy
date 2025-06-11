// Copyright 2020-2025 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package tenants

import (
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

type get struct {
	capsuleLabel string
	client       client.Reader
	log          logr.Logger
	gk           schema.GroupVersionKind
}

func Get(client client.Reader) modules.Module {
	label, _ := capsulev1beta2.GetTypeLabel(&capsulev1beta2.Tenant{})

	return &get{
		capsuleLabel: label,
		client:       client,
		log:          ctrl.Log.WithName("tenant_get"),
		gk: schema.GroupVersionKind{
			Group:   "capsule.clastix.io",
			Version: "*",
			Kind:    "tenants",
		},
	}
}

func (g get) GroupVersionKind() schema.GroupVersionKind {
	return g.gk
}

func (g get) GroupKind() schema.GroupKind {
	return g.gk.GroupKind()
}

func (g get) Path() string {
	return "/apis/{}/v1beta2/tenants/{name}"
}

func (g get) Methods() []string {
	return []string{http.MethodGet}
}

func (g get) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	name := mux.Vars(proxyRequest.GetHTTPRequest())["name"]

	userTenants := sets.New[string]()

	for _, tnt := range proxyTenants {
		userTenants.Insert(tnt.Tenant.Name)
	}

	if userTenants.Has(name) {
		return labels.NewSelector(), nil
	}

	return nil, errors.NewNotFoundError(name, g.GroupKind())
}
