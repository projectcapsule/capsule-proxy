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
}

func Get(client client.Reader) modules.Module {
	return &get{client: client, log: ctrl.Log.WithName("runtimeclass_get")}
}

func (g get) Path() string {
	return "/apis/node.k8s.io/v1/{endpoint:runtimeclasses}/{name}"
}

func (g get) Methods() []string {
	return []string{}
}

func (g get) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	name, kind := mux.Vars(httpRequest)["name"], mux.Vars(httpRequest)["endpoint"]

	_, requirements := getRuntimeClass(httpRequest, proxyTenants)
	if len(requirements) == 0 {
		return nil, errors.NewNotFoundError(
			fmt.Sprintf("%s.%s %q not found", kind, nodev1.GroupName, name),
			&metav1.StatusDetails{
				Name:  name,
				Group: nodev1.GroupName,
				Kind:  kind,
			},
		)
	}

	rc := &nodev1.RuntimeClass{}
	rc.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   nodev1.GroupName,
		Version: nodev1.SchemeGroupVersion.Version,
		Kind:    kind,
	})

	return utils.HandleGetSelector(httpRequest.Context(), rc, g.client, requirements, name, kind)
}
