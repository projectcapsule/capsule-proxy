// Copyright 2020-2025 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	"k8s.io/apimachinery/pkg/util/sets"
)

func CheckPaths(log logr.Logger, allowedPaths sets.Set[string], skipTo func(writer http.ResponseWriter, request *http.Request)) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if allowedPaths.Has(request.URL.Path) {
				log.V(4).Info("allowed url path.", "url path", request.URL.Path)
				skipTo(writer, request)

				return
			}

			next.ServeHTTP(writer, request)
		})
	}
}
