// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package lease

import (
	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/modules"
	"github.com/clastix/capsule-proxy/internal/request"
	"github.com/clastix/capsule-proxy/internal/tenant"
)

type get struct {
	client client.Reader
	log    logr.Logger
}

func Get(client client.Reader) modules.Module {
	return &get{client: client, log: ctrl.Log.WithName("node_get")}
}

func (g get) Path() string {
	return "/apis/coordination.k8s.io/v1/namespaces/kube-node-lease/leases/{name}"
}

func (g get) Methods() []string {
	return []string{"get"}
}

func (g get) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	var selectors []map[string]string

	httpRequest := proxyRequest.GetHTTPRequest()

	for _, pt := range proxyTenants {
		if ok := pt.RequestAllowed(httpRequest, capsulev1beta2.NodesProxy); ok {
			selectors = append(selectors, pt.Tenant.Spec.NodeSelector)
		}
	}

	name := mux.Vars(httpRequest)["name"]

	node := &corev1.Node{}
	if err = g.client.Get(httpRequest.Context(), types.NamespacedName{Name: name}, node); err != nil {
		// offload failure to Kubernetes API due to missing RBAC
		return nil, nil
	}

	for _, sel := range selectors {
		for k := range sel {
			if sel[k] == node.GetLabels()[k] {
				// We're matching the nodeSelector of the Tenant:
				// adding an empty selector in order to decorate the request
				return labels.NewSelector().Add(), nil
			}
		}
	}
	// requesting lease for a non owner Node: let Kubernetes deal with it
	return nil, nil
}
