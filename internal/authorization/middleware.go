package authorization

import (
	"fmt"
	"strings"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"

	v1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/modules/clusterscoped"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
	"github.com/projectcapsule/capsule-proxy/internal/types"
)

// listVerb and watchVerb are the lowercase RBAC verbs used to advertise the
// ability to list/watch resources. Note that types.ListVerb is "List"
// (capitalised) which does not match the verbs sent by clients (e.g. kubectl),
// hence dedicated constants.
const (
	listVerb                    = "list"
	watchVerb                   = "watch"
	selfSubjectAccessReviewKind = "SelfSubjectAccessReview"
	selfSubjectRulesReviewKind  = "SelfSubjectRulesReview"
)

// NamespacedResourceKey builds the lookup key identifying a namespaced resource
// that capsule-proxy proxies. It matches the (group, plural resource) pair
// carried by SelfSubjectAccessReview resource attributes.
func NamespacedResourceKey(group, resource string) string {
	return group + "/" + resource
}

// isCrossNamespaceListVerb reports whether the verb is a list or watch, i.e. the
// verbs used by `kubectl get <resource> -A` (list) and `-A -w` (watch).
func isCrossNamespaceListVerb(verb string) bool {
	return strings.EqualFold(verb, listVerb) || strings.EqualFold(verb, watchVerb)
}

// MutateAuthorization augments the API server's answer to self review requests
// (SelfSubjectAccessReview / SelfSubjectRulesReview) with the capabilities
// capsule-proxy adds on top of native RBAC, so that clients such as
// `kubectl auth can-i` reflect what actually works through the proxy.
func MutateAuthorization(proxyClusterScoped bool, proxyTenants []*tenant.ProxyTenant, namespacedResources sets.Set[string], resources []schema.GroupVersionResource, obj *runtime.Object, gvk schema.GroupVersionKind) error {
	switch gvk.Kind {
	case selfSubjectAccessReviewKind:
		//nolint:forcetypeassert
		accessReview := (*obj).(*authorizationv1.SelfSubjectAccessReview)

		attributes := accessReview.Spec.ResourceAttributes
		if attributes == nil {
			return nil
		}

		// capsule-proxy always lets tenant owners list their own namespaces.
		if attributes.Resource == types.Namespaces && strings.EqualFold(attributes.Verb, listVerb) {
			resourceGVK := schema.GroupVersionKind{Version: types.V1, Kind: types.Namespaces}
			selectors := clusterscoped.GetClusterScopeSelectors(&resourceGVK, v1beta1.ClusterResourceOperationList, proxyTenants)

			if _, err := clusterscoped.UnionSelectors(selectors); err == nil {
				grantAccess(accessReview)
			}

			return nil
		}

		// capsule-proxy serves cross-namespace (`-A`) list/watch of any proxied
		// namespaced resource for tenant owners, transparently scoping the
		// result to the tenant namespaces. The native API server denies such a
		// cluster-scoped request, so advertise the capability here to reflect
		// what actually works through the proxy (e.g. `kubectl get pods -A`).
		if len(proxyTenants) > 0 &&
			attributes.Namespace == "" &&
			isCrossNamespaceListVerb(attributes.Verb) &&
			namespacedResources.Has(NamespacedResourceKey(attributes.Group, attributes.Resource)) {
			grantAccess(accessReview)

			return nil
		}

		if !proxyClusterScoped {
			return nil
		}

		accessReviewGvk := schema.GroupVersionKind{
			Group:   attributes.Group,
			Version: attributes.Version,
			Kind:    attributes.Resource,
		}

		operation, supported := clusterResourceOperation(attributes.Verb)
		if !supported {
			return nil
		}

		if clusterscoped.HasClusterScopeAccess(&accessReviewGvk, operation, proxyTenants) {
			grantAccess(accessReview)
		}
	case selfSubjectRulesReviewKind:
		//nolint:forcetypeassert
		rules := (*obj).(*authorizationv1.SelfSubjectRulesReview)

		resourceGVK := schema.GroupVersionKind{Version: types.V1, Kind: types.Namespaces}
		selectors := clusterscoped.GetClusterScopeSelectors(&resourceGVK, v1beta1.ClusterResourceOperationList, proxyTenants)

		// Keep native rules intact and advertise namespace LIST only when its
		// global selection can actually be forwarded by the namespace module.
		if _, err := clusterscoped.UnionSelectors(selectors); err == nil {
			rules.Status.ResourceRules = append(rules.Status.ResourceRules, authorizationv1.ResourceRule{
				APIGroups: []string{""}, Resources: []string{types.Namespaces}, Verbs: []string{listVerb},
			})
		}

		if proxyClusterScoped {
			return MutateGlobalAuthorization(proxyTenants, resources, obj, gvk)
		}
	default:
		return fmt.Errorf("unsupported kind: %s", gvk.Kind)
	}

	return nil
}

// grantAccess marks a SelfSubjectAccessReview as allowed by capsule-proxy,
// clearing any denial coming from the API server.
func grantAccess(accessReview *authorizationv1.SelfSubjectAccessReview) {
	accessReview.Status.Allowed = true
	accessReview.Status.Denied = false
	accessReview.Status.Reason = "granted by capsule-proxy"
}

func clusterResourceOperation(verb string) (v1beta1.ClusterResourceOperation, bool) {
	switch {
	case strings.EqualFold(verb, listVerb):
		return v1beta1.ClusterResourceOperationList, true
	case strings.EqualFold(verb, "get"):
		return v1beta1.ClusterResourceOperationGet, true
	default:
		return "", false
	}
}

// MutateGlobalAuthorization augments self-reviews only for selectable global
// grants on cluster-resource routes registered from API discovery.
func MutateGlobalAuthorization(proxyTenants []*tenant.ProxyTenant, resources []schema.GroupVersionResource, obj *runtime.Object, gvk schema.GroupVersionKind) error {
	switch gvk.Kind {
	case selfSubjectAccessReviewKind:
		//nolint:forcetypeassert
		review := (*obj).(*authorizationv1.SelfSubjectAccessReview)

		attributes := review.Spec.ResourceAttributes
		if attributes == nil || attributes.Namespace != "" || attributes.Subresource != "" {
			return nil
		}

		operation, supported := clusterResourceOperation(attributes.Verb)
		if !supported {
			return nil
		}

		for _, resource := range resources {
			if resource.Group != attributes.Group || resource.Resource != attributes.Resource || (attributes.Version != "" && attributes.Version != "*" && resource.Version != attributes.Version) {
				continue
			}

			resourceGVK := schema.GroupVersionKind{Group: resource.Group, Version: resource.Version, Kind: resource.Resource}
			if clusterscoped.HasClusterScopeAccess(&resourceGVK, operation, proxyTenants) {
				grantAccess(review)

				break
			}
		}
	case selfSubjectRulesReviewKind:
		//nolint:forcetypeassert
		review := (*obj).(*authorizationv1.SelfSubjectRulesReview)

		for _, resource := range resources {
			resourceGVK := schema.GroupVersionKind{Group: resource.Group, Version: resource.Version, Kind: resource.Resource}

			var verbs []string

			for _, operation := range []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationList, v1beta1.ClusterResourceOperationGet} {
				if clusterscoped.HasClusterScopeAccess(&resourceGVK, operation, proxyTenants) {
					verbs = append(verbs, strings.ToLower(operation.String()))
				}
			}

			if len(verbs) > 0 {
				review.Status.ResourceRules = append(review.Status.ResourceRules, authorizationv1.ResourceRule{APIGroups: []string{resource.Group}, Resources: []string{resource.Resource}, Verbs: verbs})
			}
		}
	default:
		return fmt.Errorf("unsupported kind: %s", gvk.Kind)
	}

	return nil
}
