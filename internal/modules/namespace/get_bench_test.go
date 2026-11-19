// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

func BenchmarkNamespaceGetForwarding(b *testing.B) {
	for _, state := range []string{"selected", "excluded", "missing"} {
		b.Run(state, func(b *testing.B) {
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				b.Fatal(err)
			}
			reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
				newNamespace("selected", "", map[string]string{"env": "shared"}),
				newNamespace("excluded", "", nil),
			).Build()
			mod := Get(nil, reader)
			httpRequest := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/"+state, nil), map[string]string{"name": state})
			request := testRequest{httpRequest}
			grants := []*tenant.ProxyTenant{globalRule(map[string]string{"env": "shared"})}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				selector, err := mod.Handle(grants, request)
				if (err == nil) != (state != "excluded") || (err == nil && selector == nil) {
					b.Fatalf("state %s: selector=%v err=%v", state, selector, err)
				}
			}
		})
	}
}
