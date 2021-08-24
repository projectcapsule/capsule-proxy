// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Error interface {
	error
	Status() *metav1.Status
}
