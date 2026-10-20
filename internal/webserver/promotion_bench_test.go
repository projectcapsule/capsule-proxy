// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"fmt"
	"net/http/httptest"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/controllers"
)

func BenchmarkPromotionNamespacePipeline(b *testing.B) {
	isolateCapsuleIdentities(b)
	for _, tenants := range []int{2, 128} {
		for _, groups := range []int{0, 16} {
			for _, mode := range []string{"configured-owner", "ordinary-user", "unowned-serviceaccount", "promoted-owner", "empty-owner"} {
				for _, parallel := range []bool{false, true} {
					b.Run(fmt.Sprintf("tenants=%d/groups=%d/%s/parallel=%v", tenants, groups, mode, parallel), func(b *testing.B) {
						f := newPromotionFixture(b, tenants, true)
						user := promotedUsername
						expected := 200
						if mode == "ordinary-user" {
							user = "ordinary-user"
							expected = 403
						}
						if mode == "configured-owner" {
							controllers.CapsuleUsers.Insert(user)
						} else {
							controllers.CapsuleUsers.Delete(user)
						}
						if mode == "unowned-serviceaccount" || mode == "empty-owner" {
							tnt := &capsulev1beta2.Tenant{}
							if err := f.client.Get(b.Context(), client.ObjectKey{Name: "tenant-a"}, tnt); err != nil {
								b.Fatal(err)
							}
							if mode == "unowned-serviceaccount" {
								tnt.Status.Owners = nil
								expected = 403
							} else {
								tnt.Status.Namespaces = nil
							}
							if err := f.client.Update(b.Context(), tnt); err != nil {
								b.Fatal(err)
							}
						}
						identityGroups := make([]string, groups)
						for i := range identityGroups {
							identityGroups[i] = fmt.Sprintf("unrelated-group-%d", i)
						}
						original := promotionRequest(b.Context(), user, identityGroups...)
						run := func() {
							req := original.Clone(b.Context())
							response := httptest.NewRecorder()
							f.handler.ServeHTTP(response, req)
							if response.Code != expected {
								b.Errorf("status=%d want=%d: %s", response.Code, expected, response.Body.String())
							}
						}
						run()
						f.reader.ownerLists.Store(0)
						f.reader.totalLists.Store(0)
						b.ReportAllocs()
						b.ResetTimer()
						if parallel {
							b.RunParallel(func(pb *testing.PB) {
								for pb.Next() {
									run()
								}
							})
						} else {
							for range b.N {
								run()
							}
						}
						b.StopTimer()
						b.ReportMetric(float64(f.reader.ownerLists.Load())/float64(b.N), "owner-LIST/op")
						b.ReportMetric(float64(f.reader.totalLists.Load())/float64(b.N), "cached-LIST/op")
					})
				}
			}
		}
	}
}
