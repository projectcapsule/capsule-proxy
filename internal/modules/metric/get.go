// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package metric

import (
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	corev1 "k8s.io/api/core/v1"
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
		log:    ctrl.Log.WithName("metric_get"),
		gk: schema.GroupKind{
			Group: corev1.GroupName,
			Kind:  "nodes",
		},
	}
}

func (g get) Path() string {
	return "/apis/metrics.k8s.io/{version}/nodes/{name}"
}

func (g get) Methods() []string {
	return []string{}
}

func (g get) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	selectors := utils.GetNodeSelectors(httpRequest, proxyTenants)

	name := mux.Vars(httpRequest)["name"]

	nl := &corev1.NodeList{}
	if err = g.client.List(httpRequest.Context(), nl, client.MatchingLabels{"kubernetes.io/hostname": name}); err != nil {
		return nil, errors.NewBadRequest(err, g.gk)
	}

	var r *labels.Requirement

	if r, err = utils.GetNodeSelector(nl, selectors); err == nil {
		return labels.NewSelector().Add(*r), nil
	}

	if httpRequest.Method == http.MethodGet {
		return nil, errors.NewNotFoundError(name, g.gk)
	}

	return nil, nil
}
