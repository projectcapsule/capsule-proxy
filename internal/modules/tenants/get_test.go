// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenants

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

func TestGetPathMatchesTenantByName(t *testing.T) {
	matched := false

	router := mux.NewRouter()
	router.Path(Get(nil).Path()).Methods(http.MethodGet).HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		matched = true

		if name := mux.Vars(request)["name"]; name != "3282" {
			t.Fatalf("expected tenant name 3282, got %q", name)
		}
	})

	request := httptest.NewRequest(http.MethodGet, "/apis/capsule.clastix.io/v1beta2/tenants/3282", nil)
	router.ServeHTTP(httptest.NewRecorder(), request)

	if !matched {
		t.Fatal("tenant get route did not match")
	}
}
