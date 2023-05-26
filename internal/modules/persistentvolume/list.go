// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package persistentvolume

import (
	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/go-logr/logr"
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

type list struct {
	client   client.Reader
	log      logr.Logger
	labelKey string
	gk       schema.GroupKind
}

func List(client client.Reader) modules.Module {
	label, _ := capsulev1beta2.GetTypeLabel(&capsulev1beta2.Tenant{})

	return &list{
		client:   client,
		log:      ctrl.Log.WithName("persistentvolume_list"),
		labelKey: label,
		gk: schema.GroupKind{
			Group: corev1.GroupName,
			Kind:  "persistentvolumes",
		},
	}
}

func (l list) Path() string {
	return "/api/v1/{endpoint:persistentvolumes/?}"
}

func (l list) Methods() []string {
	return []string{}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	allowed, requirement := getPersistentVolume(httpRequest, proxyTenants, l.labelKey)
	if !allowed {
		return nil, errors.NewNotAllowed(l.gk)
	}

	return utils.HandleListSelector([]labels.Requirement{requirement})
}
