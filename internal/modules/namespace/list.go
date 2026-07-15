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
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/controllers"
	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
	"github.com/projectcapsule/capsule-proxy/internal/types"
)

type list struct {
	roleBindingsReflector *controllers.RoleBindingReflector
	reader                client.Reader
	log                   logr.Logger
	gk                    schema.GroupVersionKind
}

func List(roleBindingsReflector *controllers.RoleBindingReflector, reader client.Reader) modules.Module {
	return &list{
		roleBindingsReflector: roleBindingsReflector,
		reader:                reader,
		log:                   ctrl.Log.WithName("namespace_list"),
		gk: schema.GroupVersionKind{
			Group:   corev1.GroupName,
			Version: "*",
			Kind:    types.Namespaces,
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

	for _, tnt := range proxyTenants {
		userNamespaces = append(userNamespaces, tnt.Tenant.Status.Namespaces...)
	}

	if l.roleBindingsReflector != nil {
		reflectedNamespaces, reflectionErr := l.roleBindingsReflector.GetUserNamespacesFromRequest(proxyRequest)
		if reflectionErr != nil {
			return nil, errors.NewBadRequest(reflectionErr, l.GroupKind())
		}

		userNamespaces = append(userNamespaces, reflectedNamespaces...)
	}

	// Namespaces can additionally be granted through cluster-scoped
	// ClusterResources rules (e.g. GlobalProxySettings or ProxySettings) that
	// select namespaces by label. This lets subjects that are not tenant owners
	// list the matching namespaces. We resolve those rules to concrete namespace
	// names and merge them, so a single name-based selector is produced.
	clusterScoped, err := clusterScopedNamespaceNames(proxyRequest.GetHTTPRequest().Context(), l.reader, proxyTenants)
	if err != nil {
		return nil, errors.NewBadRequest(err, l.GroupKind())
	}

	userNamespaces = append(userNamespaces, clusterScoped...)

	var r *labels.Requirement

	switch {
	case len(userNamespaces) > 0:
		r, err = labels.NewRequirement(corev1.LabelMetadataName, selection.In, sets.List(sets.New(userNamespaces...)))
	default:
		r, err = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	if err != nil {
		return nil, errors.NewBadRequest(err, l.GroupKind())
	}

	return labels.NewSelector().Add(*r), nil
}
