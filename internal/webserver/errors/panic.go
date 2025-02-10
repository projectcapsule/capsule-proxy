// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"encoding/json"
	"fmt"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func HandleUnauthorized(w http.ResponseWriter, err error, message string) {
	message = fmt.Sprintf("%s: %s", message, err.Error())
	status := &metav1.Status{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Status",
			APIVersion: "v1",
		},
		Status:  metav1.StatusFailure,
		Message: message,
		Reason:  metav1.StatusReasonForbidden,
		Code:    http.StatusForbidden,
	}

	w.Header().Set("content-type", "application/json")

	//nolint:errchkjson
	b, _ := json.Marshal(status)
	_, _ = w.Write(b)

	panic(message)
}

func HandleError(w http.ResponseWriter, err error, message string) {
	message = fmt.Sprintf("%s: %s", message, err.Error())
	status := &metav1.Status{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Status",
			APIVersion: "v1",
		},
		Message: message,
		Reason:  metav1.StatusReasonInternalError,
	}

	w.Header().Set("content-type", "application/json")

	//nolint:errchkjson
	b, _ := json.Marshal(status)
	_, _ = w.Write(b)

	panic(message)
}
