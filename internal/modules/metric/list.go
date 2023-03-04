// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package metric

import (
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
	client client.Client
	log    logr.Logger
}

func List(client client.Client) modules.Module {
	return &list{client: client, log: ctrl.Log.WithName("metric_list")}
}

func (l list) Path() string {
	return "/apis/metrics.k8s.io/{version}/{endpoint:nodes/?}"
}

func (l list) Methods() []string {
	return []string{}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	selectors := utils.GetNodeSelectors(httpRequest, proxyTenants)

	nl := &corev1.NodeList{}
	if err = l.client.List(httpRequest.Context(), nl); err != nil {
		return nil, errors.NewBadRequest(err, &metav1.StatusDetails{Kind: "nodes"})
	}

	var r *labels.Requirement
	if r, err = utils.GetNodeSelector(nl, selectors); err != nil {
		r, _ = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	return labels.NewSelector().Add(*r), nil
}
