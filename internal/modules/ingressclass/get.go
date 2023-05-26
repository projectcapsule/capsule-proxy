// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingressclass

import (
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/modules"
	"github.com/clastix/capsule-proxy/internal/modules/errors"
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
		return g.handleSelector(httpRequest, requirements, name)
	}

	var ic client.ObjectList

	if ic, err = getIngressClassListFromRequest(httpRequest); err != nil {
		return nil, errors.NewBadRequest(err, g.gk)
	}

	if err = g.client.List(httpRequest.Context(), ic, client.MatchingLabels{"name": name}); err != nil {
		return nil, errors.NewBadRequest(err, g.gk)
	}

	var r *labels.Requirement

	if r, err = getIngressClassSelector(ic, exactMatch, regexMatch); err == nil {
		return labels.NewSelector().Add(*r), nil
	}

	switch httpRequest.Method {
	case http.MethodGet:
		return nil, errors.NewNotFoundError(name, g.gk)
	default:
		return nil, nil
	}
}

func (g get) handleSelector(request *http.Request, requirements []labels.Requirement, name string) (labels.Selector, error) {
	ic, err := getIngressClassFromRequest(request)
	if err != nil {
		br := errors.NewBadRequest(err, g.gk)
		//nolint:wrapcheck
		return nil, br
	}

	if err = g.client.Get(request.Context(), types.NamespacedName{Name: name}, ic); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errors.NewNotFoundError(name, g.gk)
		}

		return nil, errors.NewBadRequest(err, g.gk)
	}

	selector := labels.NewSelector()

	for _, requirement := range requirements {
		selector = selector.Add(requirement)
	}

	if !selector.Matches(labels.Set(ic.GetLabels())) {
		return nil, errors.NewNotFoundError(name, g.gk)
	}

	return selector, nil
}
