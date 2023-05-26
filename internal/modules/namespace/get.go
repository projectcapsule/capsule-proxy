// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"net/http"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	corev1 "k8s.io/api/core/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	capsuleLabel string
	client       client.Reader
	log          logr.Logger
	rbReflector  *controllers.RoleBindingReflector
	gk           schema.GroupKind
}

func Get(roleBindingsReflector *controllers.RoleBindingReflector, client client.Reader) modules.Module {
	label, _ := capsulev1beta2.GetTypeLabel(&capsulev1beta2.Tenant{})

	return &get{
		capsuleLabel: label,
		client:       client,
		log:          ctrl.Log.WithName("namespace_get"),
		rbReflector:  roleBindingsReflector,
		gk: schema.GroupKind{
			Group: corev1.GroupName,
			Kind:  "namespaces",
		},
	}
}

func (l get) Path() string {
	return "/api/v1/namespaces/{name}"
}

func (l get) Methods() []string {
	return []string{http.MethodGet}
}

func (l get) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	name := mux.Vars(proxyRequest.GetHTTPRequest())["name"]
	// Namespace must be retrieved: according to the strategy implemented by the proxy (cached, or API reader)
	// we need to check if the resource belongs to one of the available Tenant resources.
	ns := &corev1.Namespace{}
	if err = l.client.Get(proxyRequest.GetHTTPRequest().Context(), types.NamespacedName{Name: name}, ns); err != nil && apierr.IsNotFound(err) {
		return labels.NewSelector(), nil
	}
	// Returning a not found if the Namespace is not owned by a Tenant resource.
	if len(ns.GetOwnerReferences()) == 0 || ns.GetOwnerReferences()[0].Kind != "Tenant" {
		return nil, errors.NewNotFoundError(name, l.gk)
	}
	// Extracting the Tenant name from the owner reference:
	// in some scenarios Capsule could lag in reconciling the Tenant resources as performing the Namespace metadata
	// reconciliation, thus, these could be outdated if a user is issuing a creation and a get retrieval in a short
	// period of time (https://github.com/clastix/capsule-proxy/issues/266)
	tntName := ns.GetOwnerReferences()[0].Name

	tenants := sets.NewString()
	for _, tnt := range proxyTenants {
		tenants.Insert(tnt.Tenant.Name)
	}

	var userNamespaces []string
	// Retrieving the list of the Namespace resources owned by this user:
	// in case of rolebinding reflector, using the local cache.
	if l.rbReflector != nil {
		if userNamespaces, err = l.rbReflector.GetUserNamespacesFromRequest(proxyRequest); err != nil {
			return nil, errors.NewBadRequest(err, l.gk)
		}

		if !sets.NewString(userNamespaces...).Has(name) {
			return nil, errors.NewNotFoundError(name, l.gk)
		}
	} else if !tenants.Has(tntName) {
		return nil, errors.NewNotFoundError(name, l.gk)
	}

	return labels.NewSelector(), nil
}
