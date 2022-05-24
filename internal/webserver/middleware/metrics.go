// Copyright 2022 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func init() {
	_ = prometheus.Register(totalRequests)
	_ = prometheus.Register(httpDuration)
}

type httpResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newHTTPResponseWriter(w http.ResponseWriter) *httpResponseWriter {
	return &httpResponseWriter{
		w,
		http.StatusOK,
	}
}

func (h *httpResponseWriter) WriteHeader(statusCode int) {
	h.statusCode = statusCode
	h.ResponseWriter.WriteHeader(statusCode)
}

var totalRequests = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "capsule_proxy_requests_total",
		Help: "Number of requests",
	},
	[]string{"path", "status"},
)

var httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name: "capsule_proxy_response_time_seconds",
	Help: "Duration of capsule proxy requests.",
}, []string{"path"})

func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := mux.CurrentRoute(r)
		path, _ := route.GetPathTemplate()

		timer := prometheus.NewTimer(httpDuration.WithLabelValues(path))

		rw := newHTTPResponseWriter(w)
		next.ServeHTTP(rw, r)

		statusCode := rw.statusCode

		totalRequests.WithLabelValues(path, strconv.Itoa(statusCode)).Inc()

		timer.ObserveDuration()
	})
}
