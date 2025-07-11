// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ingressclass

import (
	"github.com/go-logr/logr"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	"github.com/projectcapsule/capsule-proxy/internal/modules/utils"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

type list struct {
	client client.Reader
	log    logr.Logger
	gk     schema.GroupVersionKind
}

func List(client client.Reader) modules.Module {
	return &list{
		client: client,
		log:    ctrl.Log.WithName("ingressclass_list"),
		gk: schema.GroupVersionKind{
			Group:   networkingv1.GroupName,
			Version: "*",
			Kind:    "ingressclasses",
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
	return "/apis/networking.k8s.io/{version}/{endpoint:ingressclasses/?}"
}

func (l list) Methods() []string {
	return []string{}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	allowed, exactMatch, regexMatch, selectorsMatch := getIngressClasses(httpRequest, proxyTenants)
	if len(selectorsMatch) > 0 {
		return utils.HandleListSelector(selectorsMatch)
	}

	icl, err := getIngressClassListFromRequest(httpRequest)
	if err != nil {
		return nil, errors.NewBadRequest(err, l.GroupKind())
	}

	if err = l.client.List(httpRequest.Context(), icl); err != nil {
		return nil, errors.NewBadRequest(err, l.GroupKind())
	}

	var r *labels.Requirement

	if r, err = getIngressClassSelector(icl, exactMatch, regexMatch); err != nil {
		if !allowed {
			return nil, errors.NewNotAllowed(l.GroupKind())
		}

		r, _ = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	return labels.NewSelector().Add(*r), nil
}
