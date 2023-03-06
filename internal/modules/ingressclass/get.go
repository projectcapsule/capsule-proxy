// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingressclass

import (
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
}

func Get(client client.Reader) modules.Module {
	return &get{client: client, log: ctrl.Log.WithName("ingressclass_get")}
}

func (g get) Path() string {
	return "/apis/networking.k8s.io/{version}/{endpoint:ingressclasses}/{name}"
}

func (g get) Methods() []string {
	return []string{}
}

func (g get) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	name, kind := mux.Vars(httpRequest)["name"], mux.Vars(httpRequest)["endpoint"]

	_, exactMatch, regexMatch, requirements := getIngressClasses(httpRequest, proxyTenants)
	if len(requirements) > 0 {
		return g.handleSelector(httpRequest, requirements, name, kind)
	}

	var ic client.ObjectList

	if ic, err = getIngressClassListFromRequest(httpRequest); err != nil {
		br := errors.NewBadRequest(err, &metav1.StatusDetails{Group: networkingv1.GroupName, Kind: kind})
		// nolint:wrapcheck
		return nil, br
	}

	if err = g.client.List(httpRequest.Context(), ic, client.MatchingLabels{"name": name}); err != nil {
		br := errors.NewBadRequest(err, &metav1.StatusDetails{Group: networkingv1.GroupName, Kind: kind})
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
			fmt.Sprintf("%s.%s %q not found", kind, networkingv1.GroupName, name),
			&metav1.StatusDetails{
				Name:  name,
				Group: networkingv1.GroupName,
				Kind:  kind,
			},
		)
		// nolint:wrapcheck
		return nil, nf
	default:
		return nil, nil
	}
}

func (g get) handleSelector(request *http.Request, requirements []labels.Requirement, name, kind string) (labels.Selector, error) {
	ic, err := getIngressClassFromRequest(request)
	if err != nil {
		br := errors.NewBadRequest(err, &metav1.StatusDetails{Group: networkingv1.GroupName, Kind: kind})
		// nolint:wrapcheck
		return nil, br
	}

	if err = g.client.Get(request.Context(), types.NamespacedName{Name: name}, ic); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, g.notFound(name, kind)
		}

		return nil, errors.NewBadRequest(err, &metav1.StatusDetails{Group: networkingv1.GroupName, Kind: kind})
	}

	selector := labels.NewSelector()

	for _, requirement := range requirements {
		selector = selector.Add(requirement)
	}

	if !selector.Matches(labels.Set(ic.GetLabels())) {
		return nil, g.notFound(name, kind)
	}

	return selector, nil
}

func (g get) notFound(name, kind string) error {
	return errors.NewNotFoundError(
		fmt.Sprintf("%s.%s %q not found", kind, networkingv1.GroupName, name),
		&metav1.StatusDetails{
			Name:  name,
			Group: networkingv1.GroupName,
			Kind:  kind,
		},
	)
}
