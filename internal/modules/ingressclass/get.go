// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingressclass

import (
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	networkingv1 "k8s.io/api/networking/v1"
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
		log:    ctrl.Log.WithName("ingressclass_get"),
		gk: schema.GroupKind{
			Group: networkingv1.GroupName,
			Kind:  "ingressclasses",
		},
	}
}

func (g get) Path() string {
	return "/apis/networking.k8s.io/{version}/{endpoint:ingressclasses}/{name}"
}

func (g get) Methods() []string {
	return []string{}
}

func (g get) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	name := mux.Vars(httpRequest)["name"]

	_, exactMatch, regexMatch, requirements := getIngressClasses(httpRequest, proxyTenants)
	if len(requirements) > 0 {
		ic, errIc := getIngressClassFromRequest(httpRequest)
		if errIc != nil {
			return nil, errors.NewBadRequest(errIc, g.gk)
		}

		return utils.HandleGetSelector(httpRequest.Context(), ic, g.client, requirements, name, g.gk)
	}

	icl, err := getIngressClassListFromRequest(httpRequest)
	if err != nil {
		return nil, errors.NewBadRequest(err, g.gk)
	}

	if err = g.client.List(httpRequest.Context(), icl, client.MatchingLabels{"name": name}); err != nil {
		return nil, errors.NewBadRequest(err, g.gk)
	}

	var r *labels.Requirement

	if r, err = getIngressClassSelector(icl, exactMatch, regexMatch); err == nil {
		return labels.NewSelector().Add(*r), nil
	}

	switch httpRequest.Method {
	case http.MethodGet:
		return nil, errors.NewNotFoundError(name, g.gk)
	default:
		return nil, nil
	}
}
