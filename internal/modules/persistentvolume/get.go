// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package persistentvolume

import (
	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/modules/utils"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

type get struct {
	client   client.Reader
	log      logr.Logger
	labelKey string
	gk       schema.GroupKind
}

func Get(client client.Reader) modules.Module {
	label, _ := capsulev1beta2.GetTypeLabel(&capsulev1beta2.Tenant{})

	return &get{
		client:   client,
		log:      ctrl.Log.WithName("persistentvolume_get"),
		labelKey: label,
		gk: schema.GroupKind{
			Group: corev1.GroupName,
			Kind:  "persistentvolumes",
		},
	}
}

func (g get) GroupKind() schema.GroupKind {
	return g.gk
}

func (g get) Path() string {
	return "/api/v1/{endpoint:persistentvolumes}/{name}"
}

func (g get) Methods() []string {
	return []string{}
}

func (g get) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	name := mux.Vars(httpRequest)["name"]

	_, requirement := getPersistentVolume(httpRequest, proxyTenants, g.labelKey)

	rc := &corev1.PersistentVolume{}

	return utils.HandleGetSelector(httpRequest.Context(), rc, g.client, []labels.Requirement{requirement}, name, g.gk)
}
