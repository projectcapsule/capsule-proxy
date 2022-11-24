// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"net/http"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/clastix/capsule-proxy/internal/controllers"
	"github.com/clastix/capsule-proxy/internal/modules"
	"github.com/clastix/capsule-proxy/internal/modules/errors"
	"github.com/clastix/capsule-proxy/internal/request"
	"github.com/clastix/capsule-proxy/internal/tenant"
)

type list struct {
	roleBindingsReflector *controllers.RoleBindingReflector
	log                   logr.Logger
}

func List(roleBindingsReflector *controllers.RoleBindingReflector) modules.Module {
	return &list{roleBindingsReflector: roleBindingsReflector, log: ctrl.Log.WithName("namespace_list")}
}

func (l list) Path() string {
	return "/api/v1/{namespaces:[^/]+/?}"
}

func (l list) Methods() []string {
	return []string{http.MethodGet}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	userNamespaces, err := l.roleBindingsReflector.GetUserNamespacesFromRequest(proxyRequest)
	if err != nil {
		return nil, errors.NewBadRequest(err, &metav1.StatusDetails{Kind: "namespaces"})
	}

	var r *labels.Requirement

	switch {
	case len(userNamespaces) > 0:
		r, err = labels.NewRequirement("name", selection.In, userNamespaces)
	default:
		r, err = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	if err != nil {
		return nil, errors.NewBadRequest(err, &metav1.StatusDetails{Kind: "namespaces"})
	}

	return labels.NewSelector().Add(*r), nil
}
