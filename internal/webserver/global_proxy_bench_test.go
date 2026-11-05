// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/controllers"
)

func BenchmarkGlobalAccessPipeline(b *testing.B) {
	isolateCapsuleIdentities(b)
	for _, unrelated := range []int{0, 128} {
		for _, groupCount := range []int{0, 16} {
			for _, mode := range []string{"default", "grant", "empty", "unmatched", "namespaced"} {
				for _, parallel := range []bool{false, true} {
					b.Run(fmt.Sprintf("unrelated=%d/groups=%d/%s/parallel=%t", unrelated, groupCount, mode, parallel), func(b *testing.B) {
						f := newGlobalAccessFixture(b, true, mode == "default", true)
						name := "tenant-b-ns"
						if mode == "empty" {
							name = "missing"
						}
						subject := "external"
						if mode == "unmatched" {
							subject = "different-user"
						}
						f.grant(b, capsulerbac.UserOwner, subject, globalNamespaceGrant(name))
						for i := range unrelated {
							setting := &v1beta1.GlobalProxySettings{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("unrelated-%d", i)}, Spec: v1beta1.GlobalProxySettingsSpec{Rules: []v1beta1.GlobalSubjectSpec{{Subjects: []v1beta1.GlobalSubject{{Kind: capsulerbac.GroupOwner, Name: fmt.Sprintf("unrelated-%d", i)}}, ClusterResources: globalNamespaceGrant(name)}}}}
							if err := f.client.Create(b.Context(), setting); err != nil {
								b.Fatal(err)
							}
							if err := f.client.Create(b.Context(), &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("unrelated-%d", i)}}); err != nil {
								b.Fatal(err)
							}
						}
						groups := make([]string, groupCount)
						for i := range groups {
							groups[i] = fmt.Sprintf("caller-group-%d", i)
						}
						original := promotionRequest(b.Context(), "external", groups...)
						if mode == "namespaced" {
							original.URL.Path = "/api/v1/pods"
						}
						want := 403
						if mode == "grant" || mode == "empty" {
							want = 200
						}
						run := func() {
							r := original.Clone(b.Context())
							response := httptest.NewRecorder()
							f.handler.ServeHTTP(response, r)
							if response.Code != want {
								b.Errorf("status=%d want=%d %s", response.Code, want, response.Body.String())
							}
						}
						run()
						f.reader.globalLists.Store(0)
						f.reader.otherLists.Store(0)
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
						b.ReportMetric(float64(f.reader.globalLists.Load())/float64(b.N), "indexed-LIST/op")
						if f.reader.otherLists.Load() != 0 {
							b.Fatal("non-Capsule requests queried tenant state")
						}
					})
				}
			}
		}
	}
}

func BenchmarkGlobalAccessSelfReview(b *testing.B) {
	isolateCapsuleIdentities(b)
	for _, mediaType := range []string{"application/json", "application/vnd.kubernetes.protobuf"} {
		for _, mode := range []string{"default", "granted", "named", "rules", "capsule-rules", "unmatched"} {
			b.Run(mediaType+"/"+mode, func(b *testing.B) {
				f := newGlobalAccessFixture(b, true, mode == "default", true)
				subject := "external-team"
				if mode == "unmatched" {
					subject = "other-team"
				}
				f.grant(b, capsulerbac.GroupOwner, subject, globalNamespaceGrant("tenant-b-ns"))
				f.enableAuthorization(b)
				var review runtime.Object = &authorizationv1.SelfSubjectAccessReview{TypeMeta: metav1.TypeMeta{APIVersion: "authorization.k8s.io/v1", Kind: "SelfSubjectAccessReview"}, Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{Resource: "namespaces", Verb: "list"}}, Status: authorizationv1.SubjectAccessReviewStatus{Denied: true}}
				if mode == "named" {
					review.(*authorizationv1.SelfSubjectAccessReview).Spec.ResourceAttributes.Verb = "get"
					review.(*authorizationv1.SelfSubjectAccessReview).Spec.ResourceAttributes.Name = "tenant-b-ns"
				}
				if mode == "capsule-rules" {
					controllers.CapsuleUsers.Insert("external")
					b.Cleanup(func() { controllers.CapsuleUsers.Delete("external") })
					for i := range 64 {
						f.filter.clusterResources = append(f.filter.clusterResources, schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: fmt.Sprintf("other-%d", i)})
					}
				}
				if mode == "rules" || mode == "capsule-rules" {
					review = &authorizationv1.SelfSubjectRulesReview{TypeMeta: metav1.TypeMeta{APIVersion: "authorization.k8s.io/v1", Kind: "SelfSubjectRulesReview"}}
				}
				var body []byte
				var err error
				if mediaType == "application/json" {
					body, err = json.Marshal(review)
				} else {
					body, err = runtime.Encode(f.filter.protoEncoder, review)
				}
				if err != nil {
					b.Fatal(err)
				}
				f.filter.reverseProxy.Transport = promotionTransport(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 201, Header: http.Header{"Content-Type": {mediaType}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
				})
				original := httptest.NewRequest(http.MethodPost, "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", nil)
				original.Header.Set("Authorization", "Bearer caller-token")
				original.Header.Set("Content-Type", "application/json")
				if mode == "capsule-rules" {
					response := httptest.NewRecorder()
					f.handler.ServeHTTP(response, original.Clone(b.Context()))
					obj, _, err := f.filter.universalDecoder.Decode(response.Body.Bytes(), nil, nil)
					if err != nil {
						b.Fatal(err)
					}
					var granted bool
					for _, rule := range obj.(*authorizationv1.SelfSubjectRulesReview).Status.ResourceRules {
						for _, resource := range rule.Resources {
							for _, verb := range rule.Verbs {
								granted = granted || resource == "namespaces" && verb == "list"
							}
						}
					}
					if !granted {
						b.Fatal("Capsule rules review omitted a valid global grant")
					}
					f.reader.globalLists.Store(0)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					response := httptest.NewRecorder()
					f.handler.ServeHTTP(response, original.Clone(b.Context()))
					if response.Code != 201 {
						b.Fatalf("status=%d %s", response.Code, response.Body.String())
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(f.reader.globalLists.Load())/float64(b.N), "indexed-LIST/op")
				if mode == "capsule-rules" {
					b.ReportMetric(float64(len(f.filter.clusterResources)), "discovered-resources")
				}
			})
		}
	}
}
