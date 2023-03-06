// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package persistentvolume

import (
	"fmt"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/modules"
	"github.com/clastix/capsule-proxy/internal/modules/errors"
	"github.com/clastix/capsule-proxy/internal/modules/utils"
	"github.com/clastix/capsule-proxy/internal/request"
	"github.com/clastix/capsule-proxy/internal/tenant"
)

type list struct {
	client   client.Reader
	log      logr.Logger
	labelKey string
}

func List(client client.Reader) modules.Module {
	label, _ := capsulev1beta2.GetTypeLabel(&capsulev1beta2.Tenant{})

	return &list{client: client, log: ctrl.Log.WithName("persistentvolume_list"), labelKey: label}
}

func (l list) Path() string {
	return "/api/v1/{endpoint:persistentvolumes/?}"
}

func (l list) Methods() []string {
	return []string{}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	kind := mux.Vars(httpRequest)["endpoint"]

	allowed, requirement := getPersistentVolume(httpRequest, proxyTenants, l.labelKey)
	if !allowed {
		return nil, errors.NewBadRequest(fmt.Errorf("not allowed"), &metav1.StatusDetails{Group: "", Kind: kind})
	}

	return utils.HandleListSelector([]labels.Requirement{requirement})
}
