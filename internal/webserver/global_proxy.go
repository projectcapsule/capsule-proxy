// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/authorization"
	"github.com/projectcapsule/capsule-proxy/internal/features"
	"github.com/projectcapsule/capsule-proxy/internal/indexer"
	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/modules/clusterscoped"
	moduleerrors "github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	"github.com/projectcapsule/capsule-proxy/internal/modules/utils"
	req "github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
	server "github.com/projectcapsule/capsule-proxy/internal/webserver/errors"
	"github.com/projectcapsule/capsule-proxy/internal/webserver/middleware"
)

// globalProxyHandler is the non-Capsule fallback. It never resolves tenant
// ownership or namespaced delegation, and never forwards with proxy credentials
// unless the requested cluster resource and operation have a selectable grant.
func (n *kubeFilter) globalProxyHandler(mod modules.Module) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if n.requireCapsuleUser || !n.gates.Enabled(features.ProxyClusterScoped) || request.Method != http.MethodGet {
			n.impersonateHandler(writer, request)

			return
		}

		gvk := utils.GetGVKFromURL(request.URL.Path)
		if gvk == nil || n.namespacedResources.Has(authorization.NamespacedResourceKey(gvk.Group, gvk.Kind)) {
			n.impersonateHandler(writer, request)

			return
		}

		if watch := request.URL.Query().Get("watch"); watch != "" {
			watching, err := strconv.ParseBool(watch)
			if err != nil || watching {
				n.impersonateHandler(writer, request)

				return
			}
		}

		request, username, groups, err := req.ResolveUserAndGroups(request, n.authTypes, n.usernameClaimField, n.writer, n.ignoredImpersonationGroups, n.impersonationGroupsRegexp, n.skipImpersonationReview, n.xfcc_header)
		if err != nil {
			n.handleResolveUserAndGroupsError(writer, err)

			return
		}

		//nolint:contextcheck // ResolveUserAndGroups preserves cancellation on the returned request.
		proxyTenants, err := n.globalProxyTenants(request.Context(), username, groups)
		if err != nil {
			server.HandleError(writer, err, "cannot retrieve GlobalProxySettings")

			return
		}

		operation := v1beta1.ClusterResourceOperationList
		if mux.Vars(request)["name"] != "" {
			operation = v1beta1.ClusterResourceOperationGet
		}

		if len(clusterscoped.GetClusterScopeSelectors(gvk, operation, proxyTenants)) == 0 {
			n.impersonateHandler(writer, request)

			return
		}

		n.handleModule(writer, request, mod, proxyTenants, username)
	}
}

// Named reviews use the same immutable discovery metadata and GET module as
// resource requests. A nonmatching object retains the native RBAC answer.
func (n *kubeFilter) globalReviewGetAllowed(ctx context.Context, proxyTenants []*tenant.ProxyTenant, obj runtime.Object) (bool, error) {
	review, ok := obj.(*authorizationv1.SelfSubjectAccessReview)
	if !ok || review.Spec.ResourceAttributes == nil {
		return true, nil
	}

	attributes := review.Spec.ResourceAttributes
	if attributes.Name == "" || !strings.EqualFold(attributes.Verb, "get") {
		return true, nil
	}

	if attributes.Namespace != "" || attributes.Subresource != "" {
		return false, nil
	}

	for _, resource := range n.clusterResources {
		if resource.Group != attributes.Group || resource.Resource != attributes.Resource || (attributes.Version != "" && attributes.Version != "*" && resource.Version != attributes.Version) {
			continue
		}

		kind, found := n.clusterResourceKinds[resource]
		if !found {
			continue
		}

		path := "/api/" + resource.Version + "/" + resource.Resource
		if resource.Group != "" {
			path = "/apis/" + resource.Group + "/" + resource.Version + "/" + resource.Resource
		}

		httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, path+"/"+url.PathEscape(attributes.Name), nil)
		if err != nil {
			return false, err
		}

		httpRequest = mux.SetURLVars(httpRequest, map[string]string{"name": attributes.Name})
		mod := clusterscoped.GetResource(kind, n.reader, n.writer, path+"/{name}")

		selector, err := mod.Handle(proxyTenants, req.NewHTTP(httpRequest, nil, "", nil, nil, nil, false, ""))
		if err != nil {
			if status, ok := errors.AsType[moduleerrors.Error](err); ok && status.Status().Code == http.StatusNotFound {
				return false, nil
			}

			return false, err
		}

		if selector != nil {
			return true, nil
		}
	}

	return false, nil
}

func (n *kubeFilter) authorizationProxyTenants(request *http.Request, username string, groups []string) ([]*tenant.ProxyTenant, bool, error) {
	if n.ignoredUsernames.Has(username) || slices.ContainsFunc(groups, n.ignoredUserGroups.Has) || n.allowedPaths.Has(request.URL.Path) {
		return nil, false, nil
	}

	if n.capsuleAvailable {
		resolved, capsuleIdentity, err := middleware.ResolveCapsuleIdentity(request, username, groups, n.managerReader)
		if err != nil {
			return nil, false, err
		}

		if capsuleIdentity {
			//nolint:contextcheck // The resolved request carries the request-local ownership snapshot.
			proxyTenants, err := n.getTenantsForOwner(resolved.Context(), username, groups)

			return proxyTenants, true, err
		}
	}

	if n.requireCapsuleUser || !n.gates.Enabled(features.ProxyClusterScoped) {
		return nil, false, nil
	}

	proxyTenants, err := n.globalProxyTenants(request.Context(), username, groups)

	return proxyTenants, false, err
}

func (n *kubeFilter) globalProxyTenants(ctx context.Context, username string, groups []string) ([]*tenant.ProxyTenant, error) {
	kind := capsulerbac.UserOwner
	if _, _, err := serviceaccount.SplitUsername(username); err == nil {
		kind = capsulerbac.ServiceAccountOwner
	}

	proxyTenants, err := n.globalProxyTenantsForSubject(ctx, kind, username, fmt.Sprintf("%s:%s", kind.String(), username))
	if err != nil {
		return nil, err
	}

	for _, group := range groups {
		groupTenants, err := n.globalProxyTenantsForSubject(ctx, capsulerbac.GroupOwner, group, fmt.Sprintf("%s:%s", capsulerbac.GroupOwner, group))
		if err != nil {
			return nil, err
		}

		proxyTenants = append(proxyTenants, groupTenants...)
	}

	return proxyTenants, nil
}

func (n *kubeFilter) globalProxyTenantsForSubject(ctx context.Context, kind capsulerbac.OwnerKind, name, indexValue string) ([]*tenant.ProxyTenant, error) {
	settings := &v1beta1.GlobalProxySettingsList{}
	if err := n.managerReader.List(ctx, settings, client.MatchingFields{indexer.GlobalKindField: indexValue}); err != nil {
		return nil, fmt.Errorf("cannot retrieve GlobalProxySettings for %s %s: %w", kind, name, err)
	}

	var proxyTenants []*tenant.ProxyTenant
	for _, setting := range settings.Items {
		proxyTenants = append(proxyTenants, tenant.NewClusterProxy(name, kind, setting.Spec.Rules))
	}

	return proxyTenants, nil
}
