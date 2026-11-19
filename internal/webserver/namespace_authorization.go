// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/projectcapsule/capsule-proxy/internal/authorization"
	moduleerrors "github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

// mutateNamespaceGetReview handles the namespace route which enforces tenant
// access even when native RBAC permits a named read. Reuse its module so global
// selectors cannot advertise every namespace and ownership/reflection still work.
func (n *kubeFilter) mutateNamespaceGetReview(ctx context.Context, proxyTenants []*tenant.ProxyTenant, obj runtime.Object) bool {
	review, ok := obj.(*authorizationv1.SelfSubjectAccessReview)
	if !ok || review.Spec.ResourceAttributes == nil {
		return false
	}

	attributes := review.Spec.ResourceAttributes
	if attributes.Group != corev1.GroupName || attributes.Resource != "namespaces" || attributes.Name == "" || !strings.EqualFold(attributes.Verb, "get") {
		return false
	}

	// These attributes do not describe the registered namespace GET route.
	// Preserve the native answer without adding a cluster-resource grant.
	if attributes.Namespace != "" || attributes.Subresource != "" || (attributes.Version != "" && attributes.Version != "*" && attributes.Version != corev1.SchemeGroupVersion.Version) {
		return true
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/namespaces/"+url.PathEscape(attributes.Name), nil)
	if err != nil {
		review.Status.Allowed, review.Status.Denied = false, false
		review.Status.EvaluationError = err.Error()

		return true
	}

	httpRequest = mux.SetURLVars(httpRequest, map[string]string{resourceNameVar: attributes.Name})
	// The context already carries the verified identity. Reflection reuses it
	// without another TokenReview or impersonation review.
	selector, err := n.namespaceGetModule.Handle(proxyTenants, request.NewHTTP(httpRequest, nil, "", nil, nil, nil, false, ""))
	if err != nil {
		review.Status.Allowed, review.Status.Denied = false, false
		review.Status.Reason = err.Error()

		if status, ok := errors.AsType[moduleerrors.Error](err); ok && status.Status().Code == http.StatusNotFound {
			review.Status.Denied = true
		} else {
			review.Status.EvaluationError = err.Error()
		}

		return true
	}

	if selector != nil {
		authorization.GrantAccess(review)
	}

	return true
}
