// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/controllers"
)

func TestGlobalAccessPreservesSelectorBoundaries(t *testing.T) {
	isolateCapsuleIdentities(t)
	for _, capsuleIdentity := range []bool{false, true} {
		for _, resource := range []string{"namespaces", "tenants", "clusterroles"} {
			t.Run(fmt.Sprintf("capsule=%t/%s", capsuleIdentity, resource), func(t *testing.T) {
				f := newGlobalAccessFixture(t, true, false, true)
				if capsuleIdentity {
					controllers.CapsuleUsers.Insert("external")
					t.Cleanup(func() { controllers.CapsuleUsers.Delete("external") })
				}
				var selected, excluded client.Object
				group, path := "", "/api/v1/namespaces"
				switch resource {
				case "namespaces":
					selected, excluded = &corev1.Namespace{}, &corev1.Namespace{}
					selected.SetName("tenant-a-ns")
					excluded.SetName("tenant-b-ns")
				case "tenants":
					selected, excluded = &capsulev1beta2.Tenant{}, &capsulev1beta2.Tenant{}
					selected.SetName("tenant-a")
					excluded.SetName("tenant-b")
					group, path = "capsule.clastix.io", "/apis/capsule.clastix.io/v1beta2/tenants"
				case "clusterroles":
					selected = &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "selected-role"}}
					excluded = &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "excluded-role"}}
					for _, obj := range []client.Object{selected, excluded} {
						if err := f.client.Create(t.Context(), obj); err != nil {
							t.Fatal(err)
						}
					}
					group, path = rbacv1.GroupName, "/apis/rbac.authorization.k8s.io/v1/clusterroles"
				}
				for i, obj := range []client.Object{selected, excluded} {
					if err := f.client.Get(t.Context(), client.ObjectKeyFromObject(obj), obj); err != nil {
						t.Fatal(err)
					}
					set := obj.GetLabels()
					if set == nil {
						set = map[string]string{}
					}
					set["tenant"], set["shared"] = []string{"a", "b"}[i], "true"
					obj.SetLabels(set)
					if err := f.client.Update(t.Context(), obj); err != nil {
						t.Fatal(err)
					}
				}
				rule := v1beta1.ClusterResource{APIGroups: []string{group}, Resources: []string{resource}, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "a", "shared": "true"}}}
				setting := f.grant(t, capsulerbac.UserOwner, "external", []v1beta1.ClusterResource{rule})
				for i, obj := range []client.Object{selected, excluded} {
					r := promotionRequest(t.Context(), "external")
					r.URL.Path = path + "/" + obj.GetName()
					response := httptest.NewRecorder()
					before := f.forwarded.Load()
					f.handler.ServeHTTP(response, r)
					if i == 0 && (response.Code != http.StatusOK || f.forwarded.Load() != before+1) {
						t.Fatalf("complete rule: %d %s", response.Code, response.Body.String())
					}
					if i == 1 && (response.Code != http.StatusForbidden && response.Code != http.StatusNotFound || f.forwarded.Load() != before) {
						t.Fatalf("partial rule forwarded with proxy credentials: %d %s", response.Code, response.Body.String())
					}
				}
				r := promotionRequest(t.Context(), "external")
				r.URL.Path = path
				response := httptest.NewRecorder()
				f.handler.ServeHTTP(response, r)
				if response.Code != http.StatusOK {
					t.Fatalf("single rule LIST: %d %s", response.Code, response.Body.String())
				}
				selector, err := labels.Parse(f.last.Load().URL.Query().Get("labelSelector"))
				if err != nil || !selector.Matches(labels.Set(selected.GetLabels())) || selector.Matches(labels.Set(excluded.GetLabels())) {
					t.Fatalf("single rule boundary lost: selector=%v error=%v", selector, err)
				}
				second := rule.DeepCopy()
				second.Selector.MatchLabels = map[string]string{"tenant": "b", "shared": "false"}
				setting.Spec.Rules[0].ClusterResources = append(setting.Spec.Rules[0].ClusterResources, *second)
				if err := f.client.Update(t.Context(), setting); err != nil {
					t.Fatal(err)
				}
				response = httptest.NewRecorder()
				before := f.forwarded.Load() + f.native.Load()
				f.handler.ServeHTTP(response, r.Clone(t.Context()))
				if response.Code != http.StatusBadRequest || f.forwarded.Load()+f.native.Load() != before {
					t.Fatalf("unsafe LIST union reached upstream: %d %s", response.Code, response.Body.String())
				}
			})
		}
	}
}
