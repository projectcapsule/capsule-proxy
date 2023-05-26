// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
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

// get is the module that is going to be used when a `kubectl describe node` is issued by a Tenant owner.
// No other verbs are considered here, just the listing of Pods for the given node.
type get struct {
	client client.Reader
	log    logr.Logger
	gk     schema.GroupKind
}

func Get(client client.Reader) modules.Module {
	return &get{
		client: client,
		log:    ctrl.Log.WithName("node_get"),
		gk: schema.GroupKind{
			Group: corev1.GroupName,
			Kind:  "nodes",
		},
	}
}

func (g get) Path() string {
	return "/api/v1/pods"
}

func (g get) Methods() []string {
	return []string{"get"}
}

func (g get) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	rawFieldSelector, ok := httpRequest.URL.Query()["fieldSelector"]
	// we want to process just the requests that are required by the kubectl describe feature and these contain the
	// field selector in the query string: if it's not there, we can skip the processing.
	if !ok || len(rawFieldSelector) == 0 {
		return nil, nil
	}

	var fieldSelector labels.Selector

	if fieldSelector, err = labels.Parse(rawFieldSelector[0]); err != nil {
		// not valid labels, offloading Kubernetes to deal with the failure
		return nil, nil
	}

	var name string

	requirements, _ := fieldSelector.Requirements()

	for _, requirement := range requirements {
		if requirement.Key() == "spec.nodeName" {
			name = requirement.Values().List()[0]

			break
		}
	}
	// the field selector is not matching any node, let Kubernetes deal the failure due to missing RBAC
	if len(name) == 0 {
		return nil, nil
	}

	var selectors []map[string]string
	// Ensuring the Tenant Owner can deal with the node listing
	for _, pt := range proxyTenants {
		if ok = pt.RequestAllowed(httpRequest, capsulev1beta2.NodesProxy); ok {
			selectors = append(selectors, pt.Tenant.Spec.NodeSelector)
		}
	}

	node := &corev1.Node{}
	if err = g.client.Get(httpRequest.Context(), types.NamespacedName{Name: name}, node); err != nil {
		return nil, errors.NewBadRequest(err, g.gk)
	}

	for _, sel := range selectors {
		for k := range sel {
			// If the node matches the label, adding an empty selector in order to decorate the request
			if sel[k] == node.GetLabels()[k] {
				return labels.NewSelector().Add(), nil
			}
		}
	}
	// offload to Kubernetes that will return the failure due to missing RBAC
	return nil, nil
}
