// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"fmt"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type notFoundError struct {
	message string
	details *metav1.StatusDetails
}

func NewNotFoundError(name string, gk schema.GroupKind) error {
	message := fmt.Sprintf("%s.%s %q not found", gk.Kind, gk.Group, name)

	return &notFoundError{
		message: message,
		details: &metav1.StatusDetails{
			Name:  name,
			Group: gk.Group,
			Kind:  gk.Kind,
		},
	}
}

func (e notFoundError) Error() string {
	return e.message
}

func (e notFoundError) Status() *metav1.Status {
	return &metav1.Status{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Status",
			APIVersion: "v1",
		},
		Reason:  metav1.StatusReasonNotFound,
		Message: e.message,
		Status:  metav1.StatusFailure,
		Code:    http.StatusNotFound,
		Details: e.details,
	}
}
