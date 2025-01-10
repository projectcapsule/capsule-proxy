// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package ingressclass

import (
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	"github.com/projectcapsule/capsule-proxy/internal/modules/utils"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

type get struct {
	client client.Reader
	log    logr.Logger
	gk     schema.GroupVersionKind
}

func Get(client client.Reader) modules.Module {
	return &get{
		client: client,
		log:    ctrl.Log.WithName("ingressclass_get"),
		gk: schema.GroupVersionKind{
			Group:   networkingv1.GroupName,
			Version: "*",
			Kind:    "ingressclasses",
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
			return nil, errors.NewBadRequest(errIc, g.GroupKind())
		}

		return utils.HandleGetSelector(httpRequest.Context(), ic, g.client, requirements, name, g.GroupKind())
	}

	icl, err := getIngressClassListFromRequest(httpRequest)
	if err != nil {
		return nil, errors.NewBadRequest(err, g.GroupKind())
	}

	if err = g.client.List(httpRequest.Context(), icl, client.MatchingLabels{corev1.LabelMetadataName: name}); err != nil {
		return nil, errors.NewBadRequest(err, g.GroupKind())
	}

	var r *labels.Requirement

	if r, err = getIngressClassSelector(icl, exactMatch, regexMatch); err == nil {
		return labels.NewSelector().Add(*r), nil
	}

	switch httpRequest.Method {
	case http.MethodGet:
		return nil, errors.NewNotFoundError(name, g.GroupKind())
	default:
		return nil, nil
	}
}
