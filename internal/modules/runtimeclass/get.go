// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package runtimeclass

import (
	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/modules"
	"github.com/clastix/capsule-proxy/internal/modules/errors"
	"github.com/clastix/capsule-proxy/internal/modules/utils"
	"github.com/clastix/capsule-proxy/internal/request"
	"github.com/clastix/capsule-proxy/internal/tenant"
)

type get struct {
	client client.Reader
	log    logr.Logger
	gk     schema.GroupKind
}

func Get(client client.Reader) modules.Module {
	return &get{
		client: client,
		log:    ctrl.Log.WithName("runtimeclass_get"),
		gk: schema.GroupKind{
			Group: nodev1.GroupName,
			Kind:  "runtimeclasses",
		},
	}
}

func (g get) Path() string {
	return "/apis/node.k8s.io/v1/{endpoint:runtimeclasses}/{name}"
}

func (g get) Methods() []string {
	return []string{}
}

func (g get) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	name := mux.Vars(httpRequest)["name"]

	_, requirements := getRuntimeClass(httpRequest, proxyTenants)
	if len(requirements) == 0 {
		return nil, errors.NewNotFoundError(name, g.gk)
	}

	rc := &nodev1.RuntimeClass{}

	return utils.HandleGetSelector(httpRequest.Context(), rc, g.client, requirements, name, g.gk)
}
