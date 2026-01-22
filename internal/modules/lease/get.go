// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package lease

import (
	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	capsuleapi "github.com/projectcapsule/capsule/pkg/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/modules"
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
		log:    ctrl.Log.WithName("node_get"),
		gk: schema.GroupVersionKind{
			Group:   corev1.GroupName,
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
	return "/apis/coordination.k8s.io/v1/namespaces/kube-node-lease/leases/{name}"
}

func (g get) Methods() []string {
	return []string{"get"}
}

func (g get) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	var selectors []map[string]string

	httpRequest := proxyRequest.GetHTTPRequest()

	for _, pt := range proxyTenants {
		if ok := pt.RequestAllowed(httpRequest, capsuleapi.NodesProxy); ok {
			selectors = append(selectors, pt.Tenant.Spec.NodeSelector)
		}
	}

	name := mux.Vars(httpRequest)["name"]

	node := &corev1.Node{}
	//nolint:nilerr
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
