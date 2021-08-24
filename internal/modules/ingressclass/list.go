// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingressclass

import (
	"context"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/modules"
	"github.com/clastix/capsule-proxy/internal/modules/errors"
	"github.com/clastix/capsule-proxy/internal/tenant"
)

type list struct {
	client client.Client
	log    logr.Logger
}

func List(client client.Client) modules.Module {
	return &list{client: client, log: ctrl.Log.WithName("ingressclass_list")}
}

func (l list) Path() string {
	return "/apis/networking.k8s.io/{version}/ingressclasses"
}

func (l list) Methods() []string {
	return []string{}
}

func (l list) Subrouter(router *mux.Router) *mux.Router {
	return router.Path("/apis/networking.k8s.io/{version}/ingressclasses").Subrouter()
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, request *http.Request) (selector labels.Selector, err error) {
	exactMatch, regexMatch := getIngressClasses(request, proxyTenants)

	var ic client.ObjectList

	if ic, err = getIngressClassFromRequest(request); err != nil {
		return nil, errors.NewBadRequest(err, &metav1.StatusDetails{Group: "networking.k8s.io", Kind: "ingressclasses"})
	}

	if err = l.client.List(context.Background(), ic); err != nil {
		return nil, errors.NewBadRequest(err, &metav1.StatusDetails{Group: "networking.k8s.io", Kind: "ingressclasses"})
	}

	var r *labels.Requirement
	if r, err = getIngressClassSelector(ic, exactMatch, regexMatch); err != nil {
		r, _ = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	return labels.NewSelector().Add(*r), nil
}
