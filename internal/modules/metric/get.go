// Copyright 2020-2025 Project Capsule Authors.
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
		log:    ctrl.Log.WithName("metric_get"),
		gk: schema.GroupVersionKind{
			Group:   "metrics.k8s.io",
			Version: "*",
			Kind:    "nodes",
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
		return nil, errors.NewBadRequest(err, g.GroupKind())
	}

	var r *labels.Requirement

	if r, err = utils.GetNodeSelector(nl, selectors); err == nil {
		return labels.NewSelector().Add(*r), nil
	}

	if httpRequest.Method == http.MethodGet {
		return nil, errors.NewNotFoundError(name, g.GroupKind())
	}

	return nil, nil
}
