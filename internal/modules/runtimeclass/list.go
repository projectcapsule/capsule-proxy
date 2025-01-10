// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package runtimeclass

import (
	"github.com/go-logr/logr"
	nodev1 "k8s.io/api/node/v1"
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
		log:    ctrl.Log.WithName("runtimeclass_list"),
		gk: schema.GroupVersionKind{
			Group:   nodev1.GroupName,
			Version: "*",
			Kind:    "runtimeclasses",
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
	return "/apis/node.k8s.io/v1/{endpoint:runtimeclasses/?}"
}

func (l list) Methods() []string {
	return []string{}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	allowed, selectorsMatch := getRuntimeClass(httpRequest, proxyTenants)

	if !allowed {
		return nil, errors.NewNotAllowed(l.GroupKind())
	}

	if len(selectorsMatch) == 0 {
		r, _ := labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})

		return labels.NewSelector().Add(*r), nil
	}

	return utils.HandleListSelector(selectorsMatch)
}
