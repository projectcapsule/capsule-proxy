// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package runtimeclass

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	nodev1 "k8s.io/api/node/v1"
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
	return &list{client: client, log: ctrl.Log.WithName("runtimeclass_list")}
}

func (l list) Path() string {
	return "/apis/node.k8s.io/v1/{endpoint:runtimeclasses/?}"
}

func (l list) Methods() []string {
	return []string{}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	kind := mux.Vars(httpRequest)["endpoint"]

	allowed, selectorsMatch := getRuntimeClass(httpRequest, proxyTenants)
	if !allowed {
		return nil, errors.NewBadRequest(fmt.Errorf("not allowed"), &metav1.StatusDetails{Group: nodev1.GroupName, Kind: kind})
	}

	if len(selectorsMatch) == 0 {
		r, _ := labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})

		return labels.NewSelector().Add(*r), nil
	}

	return utils.HandleListSelector(selectorsMatch)
}
