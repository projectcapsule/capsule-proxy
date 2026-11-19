// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"encoding/json"
	"fmt"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

// Measure the proxy's self-review transformation separately from upstream I/O
// and identity/settings resolution, which this change leaves untouched.
func BenchmarkNamespaceGetSelfReview(b *testing.B) {
	for _, count := range []int{1, 32} {
		for _, mode := range []string{"selected", "excluded", "owner", "list"} {
			b.Run(fmt.Sprintf("tenants=%d/%s", count, mode), func(b *testing.B) {
				f := newGlobalAccessFixture(b, true, true, true)
				reader := &namespaceReviewReader{Reader: f.client}
				f.filter.reader = reader
				f.routes()
				f.enableAuthorization(b)
				proxyTenants := make([]*tenant.ProxyTenant, count)
				for i := range proxyTenants {
					proxyTenants[i] = &tenant.ProxyTenant{Tenant: capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("unrelated-%d", i)}}, ClusterResources: globalNamespaceGrant("tenant-b-ns")}
				}
				attributes := &authorizationv1.ResourceAttributes{Version: "v1", Resource: "namespaces", Verb: "get", Name: "tenant-b-ns"}
				if mode == "excluded" || mode == "owner" {
					attributes.Name = "tenant-a-ns"
				}
				if mode == "owner" {
					proxyTenants[count-1].Tenant.Name = "tenant-a"
				}
				if mode == "list" {
					attributes.Verb, attributes.Name = "list", ""
				}
				body, err := json.Marshal(&authorizationv1.SelfSubjectAccessReview{TypeMeta: metav1.TypeMeta{APIVersion: "authorization.k8s.io/v1", Kind: "SelfSubjectAccessReview"}, Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: attributes}})
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				var result []byte
				for range b.N {
					result, err = f.filter.mutateAuthorizationBody(b.Context(), body, "application/json", proxyTenants, true)
					if err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				var review authorizationv1.SelfSubjectAccessReview
				if err := json.Unmarshal(result, &review); err != nil || review.Status.Allowed != (mode != "excluded") {
					b.Fatalf("invalid review: %+v, %v", review.Status, err)
				}
				b.ReportMetric(float64(reader.reads)/float64(b.N), "namespace-GET/op")
			})
		}
	}
}
