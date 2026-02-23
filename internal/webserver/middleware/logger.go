// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
)

func LoggerMiddleware(log logr.Logger) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.V(10).Info("HTTP details",
				"method", r.Method,
				"url", r.URL.String(),
				"host", r.Host,
				"headers", r.Header,
				"remoteAddr", r.RemoteAddr,
				"requestURI", r.RequestURI,
				"proto", r.Proto,
				"contentLength", r.ContentLength,
				"transferEncoding", r.TransferEncoding,
				"cookies", r.Cookies(),
				"query", r.URL.Query(),
			)

			next.ServeHTTP(w, r)
		})
	}
}
