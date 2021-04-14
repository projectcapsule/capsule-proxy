package errors

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Error interface {
	error
	Status() *metav1.Status
}
