// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type badRequest struct {
	message string
	details *metav1.StatusDetails
}

func NewBadRequest(message error, gk schema.GroupKind) error {
	return &badRequest{
		message: message.Error(),
		details: &metav1.StatusDetails{
			Group: gk.Group,
			Kind:  gk.Kind,
		},
	}
}

func (b badRequest) Error() string {
	return b.message
}

func (b badRequest) Status() *metav1.Status {
	return &metav1.Status{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Status",
			APIVersion: "v1",
		},
		Reason:  metav1.StatusReasonBadRequest,
		Message: b.message,
		Status:  metav1.StatusFailure,
		Code:    http.StatusBadRequest,
		Details: b.details,
	}
}
