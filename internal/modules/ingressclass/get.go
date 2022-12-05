// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingressclass

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/modules"
	"github.com/clastix/capsule-proxy/internal/modules/errors"
	"github.com/clastix/capsule-proxy/internal/request"
	"github.com/clastix/capsule-proxy/internal/tenant"
)

type get struct {
	client client.Client
	log    logr.Logger
}

func Get(client client.Client) modules.Module {
	return &get{client: client, log: ctrl.Log.WithName("ingressclass_get")}
}

func (g get) Path() string {
	return "/apis/networking.k8s.io/{version}/ingressclasses/{name}"
}

func (g get) Methods() []string {
	return []string{}
}

func (g get) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()
	_, exactMatch, regexMatch := getIngressClasses(httpRequest, proxyTenants)

	name := mux.Vars(httpRequest)["name"]

	var ic client.ObjectList

	if ic, err = getIngressClassFromRequest(httpRequest); err != nil {
		br := errors.NewBadRequest(err, &metav1.StatusDetails{Group: "networking.k8s.io", Kind: "ingressclasses"})
		// nolint:wrapcheck
		return nil, br
	}

	if err = g.client.List(context.Background(), ic, client.MatchingLabels{"name": name}); err != nil {
		br := errors.NewBadRequest(err, &metav1.StatusDetails{Group: "networking.k8s.io", Kind: "ingressclasses"})
		// nolint:wrapcheck
		return nil, br
	}

	var r *labels.Requirement

	if r, err = getIngressClassSelector(ic, exactMatch, regexMatch); err == nil {
		return labels.NewSelector().Add(*r), nil
	}

	switch httpRequest.Method {
	case http.MethodGet:
		nf := errors.NewNotFoundError(
			fmt.Sprintf("ingressclasses.networking.k8s.io \"%s\" not found", name),
			&metav1.StatusDetails{
				Name:  name,
				Group: "networking.k8s.io",
				Kind:  "ingressclasses",
			},
		)
		// nolint:wrapcheck
		return nil, nf
	default:
		return nil, nil
	}
}
