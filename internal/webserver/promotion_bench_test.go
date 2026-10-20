// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"fmt"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/controllers"
	"github.com/projectcapsule/capsule-proxy/internal/modules/namespaced"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
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

func BenchmarkNamespacedListEmptyAccess(b *testing.B) {
	for _, count := range []int{1, 32} {
		for _, mode := range []string{"allowed", "denied", "no-namespaces"} {
			for _, parallel := range []bool{false, true} {
				b.Run(fmt.Sprintf("tenants=%d/%s/parallel=%v", count, mode, parallel), func(b *testing.B) {
					f := newPromotionFixture(b, 2, true)
					profiles := make([]*tenant.ProxyTenant, count)
					for i := range profiles {
						tnt := capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("tenant-%d", i)}}
						if mode != "no-namespaces" {
							tnt.Status.Namespaces = []string{tnt.Name + "-ns"}
						}
						profiles[i] = &tenant.ProxyTenant{Tenant: tnt}
					}
					var reviews atomic.Int64
					f.reviews.authorize = func(sar *authorizationv1.SubjectAccessReview) error {
						reviews.Add(1)
						sar.Status.Allowed = mode == "allowed"
						return nil
					}
					resolved, _, _, err := request.ResolveUserAndGroups(promotionRequest(b.Context(), promotedUsername), f.filter.authTypes, "", f.filter.writer, nil, nil, false, "")
					if err != nil {
						b.Fatal(err)
					}
					proxyRequest := request.NewHTTP(resolved, f.filter.authTypes, "", f.filter.writer, nil, nil, false, "")
					mod := namespaced.CatchAll(f.filter.writer, nil, "/api/v1/secrets", "", "v1", "secrets")
					own := labels.Set{capsulemeta.ManagedByCapsuleLabel: "tenant-0"}
					run := func() {
						selector, handleErr := mod.Handle(profiles, proxyRequest)
						if handleErr != nil || selector == nil || selector.String() == "" {
							b.Errorf("invalid selection: %v", handleErr)
							return
						}
						if selector.Matches(own) != (mode == "allowed") {
							b.Errorf("incorrect selection for %s", mode)
						}
					}
					run()
					reviews.Store(0)
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
					b.ReportMetric(float64(reviews.Load())/float64(b.N), "SAR/op")
					expected := int64(b.N) * int64(count)
					if mode == "no-namespaces" {
						expected = 0
					}
					if reviews.Load() != expected {
						b.Errorf("reviews=%d expected=%d", reviews.Load(), expected)
					}
				})
			}
		}
	}
}
