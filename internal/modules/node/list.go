// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/modules"
	"github.com/clastix/capsule-proxy/internal/modules/errors"
	"github.com/clastix/capsule-proxy/internal/modules/utils"
	"github.com/clastix/capsule-proxy/internal/request"
	"github.com/clastix/capsule-proxy/internal/tenant"
)

type list struct {
	client client.Reader
	log    logr.Logger
	gk     schema.GroupKind
}

func List(client client.Reader) modules.Module {
	return &list{
		client: client,
		log:    ctrl.Log.WithName("node_list"),
		gk: schema.GroupKind{
			Group: corev1.GroupName,
			Kind:  "nodes",
		},
	}
}

func (l list) Path() string {
	return "/api/v1/{endpoint:nodes/?}"
}

func (l list) Methods() []string {
	return []string{}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()
	selectors := utils.GetNodeSelectors(httpRequest, proxyTenants)

	nl := &corev1.NodeList{}
	if err = l.client.List(httpRequest.Context(), nl); err != nil {
		return nil, errors.NewBadRequest(err, l.gk)
	}

	var r *labels.Requirement
	if r, err = utils.GetNodeSelector(nl, selectors); err != nil {
		r, _ = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	return labels.NewSelector().Add(*r), nil
}
