package authorization

import (
	"fmt"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func MutateAuthorization(obj *runtime.Object, gvk schema.GroupVersionKind) error {
	switch gvk.Kind {
	case "SelfSubjectAccessReview":
		accessReview := (*obj).(*authorizationv1.SelfSubjectAccessReview)
		if accessReview.Spec.ResourceAttributes.Resource == "namespaces" && accessReview.Spec.ResourceAttributes.Verb == "list" {
			accessReview.Status.Allowed = true
		}
	case "SelfSubjectRulesReview":
		rules := (*obj).(*authorizationv1.SelfSubjectRulesReview)
		src := authorizationv1.SelfSubjectRulesReview{
			Status: authorizationv1.SubjectRulesReviewStatus{
				ResourceRules: []authorizationv1.ResourceRule{
					{APIGroups: []string{""},
						Resources: []string{"namespaces"},
						Verbs:     []string{"list"},
					},
				},
			},
		}
		rules.XXX_Merge(&src)
	default:
		return fmt.Errorf("unsupported kind: %s", gvk.Kind)
	}
	return nil
}
