// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"net/http"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/projectcapsule/capsule-proxy/internal/controllers"
	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

type list struct {
	roleBindingsReflector *controllers.RoleBindingReflector
	log                   logr.Logger
	gk                    schema.GroupVersionKind
}

func List(roleBindingsReflector *controllers.RoleBindingReflector) modules.Module {
	return &list{
		roleBindingsReflector: roleBindingsReflector,
		log:                   ctrl.Log.WithName("namespace_list"),
		gk: schema.GroupVersionKind{
			Group:   corev1.GroupName,
			Version: "*",
			Kind:    "namespaces",
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
	return basePath
}

func (l list) Methods() []string {
	return []string{http.MethodGet}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	var userNamespaces []string

	if l.roleBindingsReflector != nil {
		userNamespaces, err = l.roleBindingsReflector.GetUserNamespacesFromRequest(proxyRequest)
		if err != nil {
			return nil, errors.NewBadRequest(err, l.GroupKind())
		}
	} else {
		for _, tnt := range proxyTenants {
			userNamespaces = append(userNamespaces, tnt.Tenant.Status.Namespaces...)
		}
	}

	var r *labels.Requirement

	switch {
	case len(userNamespaces) > 0:
		r, err = labels.NewRequirement(corev1.LabelMetadataName, selection.In, userNamespaces)
	default:
		r, err = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	if err != nil {
		return nil, errors.NewBadRequest(err, l.GroupKind())
	}

	return labels.NewSelector().Add(*r), nil
}
