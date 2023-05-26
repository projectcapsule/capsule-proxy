package errors

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func NewNotAllowed(gk schema.GroupKind) error {
	return &badRequest{
		message: "not allowed",
		details: &metav1.StatusDetails{
			Group: gk.Group,
			Kind:  gk.Kind,
		},
	}
}
