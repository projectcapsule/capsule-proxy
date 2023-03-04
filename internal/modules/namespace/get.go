// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	corev1 "k8s.io/api/core/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/controllers"
	"github.com/clastix/capsule-proxy/internal/modules"
	"github.com/clastix/capsule-proxy/internal/modules/errors"
	"github.com/clastix/capsule-proxy/internal/request"
	"github.com/clastix/capsule-proxy/internal/tenant"
)

type get struct {
	roleBindingsReflector *controllers.RoleBindingReflector
	log                   logr.Logger
	client                client.Reader
}

func Get(roleBindingsReflector *controllers.RoleBindingReflector, client client.Reader) modules.Module {
	return &get{roleBindingsReflector: roleBindingsReflector, log: ctrl.Log.WithName("namespace_get"), client: client}
}

func (l get) Path() string {
	return "/api/v1/namespaces/{name}"
}

func (l get) Methods() []string {
	return []string{http.MethodGet}
}

func (l get) Handle(_ []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	name := mux.Vars(proxyRequest.GetHTTPRequest())["name"]

	if err = l.client.Get(proxyRequest.GetHTTPRequest().Context(), types.NamespacedName{Name: name}, &corev1.Namespace{}); err != nil && apierr.IsNotFound(err) {
		return labels.NewSelector(), nil
	}

	var userNamespaces []string
	// Retrieving the list of the Namespace resources owned by this user
	if userNamespaces, err = l.roleBindingsReflector.GetUserNamespacesFromRequest(proxyRequest); err != nil {
		return nil, errors.NewBadRequest(err, &metav1.StatusDetails{Kind: "namespaces"})
	}

	if !sets.NewString(userNamespaces...).Has(name) {
		return nil, errors.NewNotFoundError(fmt.Sprintf("namespace %q not found", name), &metav1.StatusDetails{
			Name:  name,
			Group: "v1",
			Kind:  "namespaces",
		})
	}

	return labels.NewSelector(), nil
}
