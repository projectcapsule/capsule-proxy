// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	"k8s.io/apimachinery/pkg/util/sets"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestPublicPathsBypassAuthenticationButRequireTrustedSource(t *testing.T) {
	t.Parallel()

	var upstreamRequests atomic.Int32

	upstreamAuthorization := make(chan string, 3)
	reverseProxy := httputil.NewSingleHostReverseProxy(&url.URL{
		Scheme: "https",
		Host:   "kubernetes.example",
	})
	reverseProxy.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		upstreamRequests.Add(1)
		upstreamAuthorization <- request.Header.Get("Authorization")

		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    request,
		}, nil
	})

	_, trustedCIDR, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}

	filter := &kubeFilter{
		allowedPaths:      sets.New("/api", "/apis", "/version"),
		publicPaths:       sets.New("/public"),
		reverseProxy:      reverseProxy,
		trustedProxyCIDRs: []*net.IPNet{trustedCIDR},
		log:               logr.Discard(),
	}

	router := mux.NewRouter()
	root := router.PathPrefix("").Subrouter()
	filter.registerRootMiddlewares(root)
	root.PathPrefix("/").HandlerFunc(filter.impersonateHandler)

	publicRequest := httptest.NewRequest(http.MethodGet, "http://proxy.example/public", nil)
	publicRequest.RemoteAddr = "10.1.2.3:1234"
	publicResponse := httptest.NewRecorder()
	router.ServeHTTP(publicResponse, publicRequest)

	if got := publicResponse.Code; got != http.StatusNoContent {
		t.Fatalf("trusted public request status = %d, want %d", got, http.StatusNoContent)
	}
	if got := <-upstreamAuthorization; got != "" {
		t.Fatalf("trusted public request Authorization = %q, want empty", got)
	}

	untrustedRequest := httptest.NewRequest(http.MethodGet, "http://proxy.example/public", nil)
	untrustedRequest.RemoteAddr = "192.0.2.1:1234"
	untrustedResponse := httptest.NewRecorder()
	router.ServeHTTP(untrustedResponse, untrustedRequest)

	if got := untrustedResponse.Code; got != http.StatusForbidden {
		t.Fatalf("untrusted public request status = %d, want %d", got, http.StatusForbidden)
	}
	if got := upstreamRequests.Load(); got != 1 {
		t.Fatalf("upstream request count = %d, want 1", got)
	}

	allowedRequest := httptest.NewRequest(http.MethodGet, "http://proxy.example/api", nil)
	allowedRequest.RemoteAddr = "10.1.2.3:1234"
	allowedResponse := httptest.NewRecorder()
	router.ServeHTTP(allowedResponse, allowedRequest)

	if got := allowedResponse.Code; got != http.StatusForbidden {
		t.Fatalf("unauthenticated allowed request status = %d, want %d", got, http.StatusForbidden)
	}
}
