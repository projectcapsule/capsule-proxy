// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type Filter interface {
	manager.Runnable
	ReadinessProbe(req *http.Request) error
	LivenessProbe(req *http.Request) error
}
