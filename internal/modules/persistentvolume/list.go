// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package persistentvolume

import (
	"github.com/go-logr/logr"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
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

type list struct {
	client   client.Reader
	log      logr.Logger
	labelKey string
	gk       schema.GroupVersionKind
}

func List(client client.Reader) modules.Module {
	label, _ := capsulev1beta2.GetTypeLabel(&capsulev1beta2.Tenant{})

	return &list{
		client:   client,
		log:      ctrl.Log.WithName("persistentvolume_list"),
		labelKey: label,
		gk: schema.GroupVersionKind{
			Group:   corev1.GroupName,
			Version: "*",
			Kind:    "persistentvolumes",
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
	return "/api/v1/{endpoint:persistentvolumes/?}"
}

func (l list) Methods() []string {
	return []string{}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	allowed, requirement := getPersistentVolume(httpRequest, proxyTenants, l.labelKey)
	if !allowed {
		return nil, errors.NewNotAllowed(l.GroupKind())
	}

	return utils.HandleListSelector([]labels.Requirement{requirement})
}
