package authorization

import (
	"fmt"
	"strings"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule-proxy/internal/modules/clusterscoped"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

func MutateAuthorization(proxyClusterScoped bool, proxyTenants []*tenant.ProxyTenant, obj *runtime.Object, gvk schema.GroupVersionKind) error {
	switch gvk.Kind {
	case "SelfSubjectAccessReview":
		//nolint:forcetypeassert
		accessReview := (*obj).(*authorizationv1.SelfSubjectAccessReview)
		if accessReview.Spec.ResourceAttributes.Resource == "namespaces" && accessReview.Spec.ResourceAttributes.Verb == "list" {
			accessReview.Status.Allowed = true
		}

		if !proxyClusterScoped {
			return nil
		}

		accessReviewGvk := schema.GroupVersionKind{
			Group:   accessReview.Spec.ResourceAttributes.Group,
			Version: accessReview.Spec.ResourceAttributes.Version,
			Kind:    accessReview.Spec.ResourceAttributes.Resource,
		}

		verbs, req := clusterscoped.GetClusterScopeRequirements(&accessReviewGvk, proxyTenants)
		if len(req) == 0 {
			return nil
		}

		for _, verb := range verbs {
			if accessReview.Spec.ResourceAttributes.Verb == strings.ToLower(verb.String()) {
				accessReview.Status.Allowed = true

				return nil
			}
		}
	case "SelfSubjectRulesReview":
		//nolint:forcetypeassert
		rules := (*obj).(*authorizationv1.SelfSubjectRulesReview)

		var resourceRules []authorizationv1.ResourceRule
		if proxyClusterScoped {
			resourceRules = getAllResourceRules(proxyTenants)
		} else {
			resourceRules = []authorizationv1.ResourceRule{}
		}

		resourceRules = append(resourceRules, authorizationv1.ResourceRule{
			APIGroups: []string{""},
			Resources: []string{"namespaces"},
			Verbs:     []string{"list"},
		})
		src := authorizationv1.SelfSubjectRulesReview{
			Status: authorizationv1.SubjectRulesReviewStatus{
				ResourceRules: resourceRules,
			},
		}
		rules.XXX_Merge(&src)
	default:
		return fmt.Errorf("unsupported kind: %s", gvk.Kind)
	}

	return nil
}

func getAllResourceRules(proxyTenants []*tenant.ProxyTenant) []authorizationv1.ResourceRule {
	resourceRules := []authorizationv1.ResourceRule{}

	for _, pt := range proxyTenants {
		for _, cr := range pt.ClusterResources {
			verbs := []string{}
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
