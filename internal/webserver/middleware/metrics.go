// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

//nolint:gochecknoinits
func init() {
	metrics.Registry.MustRegister(totalRequests, httpDuration)
}

type httpResponseWriter struct {
	http.ResponseWriter

	statusCode int
	written    bool
}

// NewHTTPResponseWriter tracks responses while preserving HTTP capabilities.
func NewHTTPResponseWriter(w http.ResponseWriter) *httpResponseWriter {
	return &httpResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (h *httpResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := h.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("writer is not http.Hijacker")
	}

	conn, buffer, err := hijacker.Hijack()
	if err == nil {
		h.written = true
	}

	return conn, buffer, err
}

func (h *httpResponseWriter) WriteHeader(statusCode int) {
	if statusCode >= http.StatusOK || statusCode == http.StatusSwitchingProtocols {
		h.written = true
	}

	h.statusCode = statusCode
	h.ResponseWriter.WriteHeader(statusCode)
}

// Write records implicit successful responses as well as explicit errors.
func (h *httpResponseWriter) Write(body []byte) (int, error) {
	h.written = true

	return h.ResponseWriter.Write(body)
}

// Written reports whether a middleware has completed the response.
func (h *httpResponseWriter) Written() bool { return h.written }

// FlushError preserves streaming and records the implicitly written headers.
func (h *httpResponseWriter) FlushError() error {
	err := http.NewResponseController(h.ResponseWriter).Flush()
	if err == nil {
		h.written = true
	}

	return err
}

// Flush preserves the http.Flusher interface for existing handlers.
func (h *httpResponseWriter) Flush() { _ = h.FlushError() }

// Unwrap preserves ResponseController flushing, streaming and protocol upgrades.
func (h *httpResponseWriter) Unwrap() http.ResponseWriter { return h.ResponseWriter }

//nolint:gochecknoglobals
var totalRequests = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "capsule_proxy_requests_total",
		Help: "Number of requests",
	},
	[]string{"path", "status"},
)

//nolint:gochecknoglobals
var httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name: "capsule_proxy_response_time_seconds",
	Help: "Duration of capsule proxy requests.",
}, []string{"path"})

func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := mux.CurrentRoute(r)
		path, _ := route.GetPathTemplate()

		timer := prometheus.NewTimer(httpDuration.WithLabelValues(path))

		rw := NewHTTPResponseWriter(w)
		next.ServeHTTP(rw, r)

		statusCode := rw.statusCode

		totalRequests.WithLabelValues(path, strconv.Itoa(statusCode)).Inc()

		timer.ObserveDuration()
	})
}
