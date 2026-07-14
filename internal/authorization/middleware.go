package authorization

import (
	"fmt"
	"strings"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/projectcapsule/capsule-proxy/internal/modules/clusterscoped"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
	"github.com/projectcapsule/capsule-proxy/internal/types"
)

// listVerb and watchVerb are the lowercase RBAC verbs used to advertise the
// ability to list/watch resources. Note that types.ListVerb is "List"
// (capitalised) which does not match the verbs sent by clients (e.g. kubectl),
// hence dedicated constants.
const (
	listVerb  = "list"
	watchVerb = "watch"
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
func MutateAuthorization(proxyClusterScoped bool, proxyTenants []*tenant.ProxyTenant, namespacedResources sets.Set[string], obj *runtime.Object, gvk schema.GroupVersionKind) error {
	switch gvk.Kind {
	case "SelfSubjectAccessReview":
		//nolint:forcetypeassert
		accessReview := (*obj).(*authorizationv1.SelfSubjectAccessReview)

		attributes := accessReview.Spec.ResourceAttributes
		if attributes == nil {
			return nil
		}

		// capsule-proxy always lets tenant owners list their own namespaces.
		if attributes.Resource == types.Namespaces && strings.EqualFold(attributes.Verb, listVerb) {
			grantAccess(accessReview)

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

		verbs, req := clusterscoped.GetClusterScopeRequirements(&accessReviewGvk, proxyTenants)
		if len(req) == 0 {
			return nil
		}

		for _, verb := range verbs {
			if strings.EqualFold(attributes.Verb, verb.String()) {
				grantAccess(accessReview)

				return nil
			}
		}
	case "SelfSubjectRulesReview":
		//nolint:forcetypeassert
		rules := (*obj).(*authorizationv1.SelfSubjectRulesReview)

		injectedRules := []authorizationv1.ResourceRule{
			{
				APIGroups: []string{""},
				Resources: []string{types.Namespaces},
				Verbs:     []string{listVerb},
			},
		}

		if proxyClusterScoped {
			injectedRules = append(injectedRules, getAllResourceRules(proxyTenants)...)
		}

		// The rules resolved by the apiserver are kept and capsule-proxy only appends,
		// so passed-through and namespaced permissions remain visible to the client.
		rules.Status.ResourceRules = append(rules.Status.ResourceRules, injectedRules...)
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

func getAllResourceRules(proxyTenants []*tenant.ProxyTenant) []authorizationv1.ResourceRule {
	resourceRules := []authorizationv1.ResourceRule{}

	for _, pt := range proxyTenants {
		for _, cr := range pt.ClusterResources {
			verbs := []string{}

			//nolint:staticcheck
			for _, op := range cr.Operations {
				verbs = append(verbs, strings.ToLower(op.String()))
			}

			resourceRules = append(resourceRules, authorizationv1.ResourceRule{
				APIGroups: cr.APIGroups,
				Resources: cr.Resources,
				Verbs:     verbs,
			})
		}
	}

	return resourceRules
}
