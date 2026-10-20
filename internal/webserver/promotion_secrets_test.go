// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/modules/namespaced"
	"github.com/projectcapsule/capsule-proxy/internal/request"
)

func TestPromotedOwnerAllNamespacesSecrets(t *testing.T) {
	isolateCapsuleIdentities(t)
	for _, gate := range []bool{false, true} {
		for _, mode := range []string{"owner", "empty", "revoked", "resource-denied", "review-error", "lookup-error", "conflicting-selector"} {
			t.Run(fmt.Sprintf("gate=%v/%s", gate, mode), func(t *testing.T) {
				f := newPromotionFixture(t, 2, gate)
				if err := f.client.Delete(t.Context(), &v1beta1.ProxySetting{ObjectMeta: metav1.ObjectMeta{Name: "self-grant", Namespace: "tenant-a-ns"}}); err != nil {
					t.Fatal(err)
				}
				f.filter.authTypes[0] = request.BearerToken
				expected, expectedNames, expectedReviews := http.StatusOK, []string{"tenant-a-ns/visible"}, 1
				switch mode {
				case "empty", "revoked":
					tnt := &capsulev1beta2.Tenant{}
					if err := f.client.Get(t.Context(), client.ObjectKey{Name: "tenant-a"}, tnt); err != nil {
						t.Fatal(err)
					}
					if mode == "empty" {
						tnt.Status.Namespaces = nil
					} else {
						tnt.Status.Owners = nil
						expected = http.StatusForbidden
					}
					if err := f.client.Update(t.Context(), tnt); err != nil {
						t.Fatal(err)
					}
					expectedNames, expectedReviews = nil, 0
				case "resource-denied", "conflicting-selector":
					expectedNames = nil
				case "review-error":
					expected = http.StatusInternalServerError
				case "lookup-error":
					f.reader.failure = errors.New("owner index unavailable")
					expected, expectedReviews = http.StatusInternalServerError, 0
				}
				reviews, upstream := 0, 0
				f.reviews.authorize = func(sar *authorizationv1.SubjectAccessReview) error {
					reviews++
					attributes := sar.Spec.ResourceAttributes
					if strings.Join(sar.Spec.Groups, ",") != "system:authenticated,system:serviceaccounts,system:serviceaccounts:tenant-a-ns" {
						t.Fatalf("incorrect ServiceAccount groups: %v", sar.Spec.Groups)
					}
					if sar.Spec.User != promotedUsername || attributes == nil || attributes.Namespace != "tenant-a-ns" || attributes.Verb != "list" || attributes.Group != "" || attributes.Version != "v1" || attributes.Resource != "secrets" {
						t.Fatalf("incorrect subject or authorization scope: %#v", sar.Spec)
					}
					if mode == "review-error" {
						return errors.New("authorization unavailable")
					}
					sar.Status.Allowed = mode != "resource-denied"
					return nil
				}
				f.setModule(namespaced.CatchAll(f.filter.writer, nil, "/api/v1/secrets", "", "v1", "secrets"))
				f.filter.reverseProxy.Transport = promotionTransport(func(req *http.Request) (*http.Response, error) {
					upstream++
					if req.URL.Path != "/api/v1/secrets" {
						t.Fatalf("unexpected path: %s", req.URL.Path)
					}
					if req.Header.Get("Impersonate-User") != "" {
						return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"kind":"Status","apiVersion":"v1","reason":"Forbidden","code":403}`)), Request: req}, nil
					}
					if req.Header.Get("Authorization") != "Bearer proxy-token" || len(req.Header.Values("Impersonate-Group")) != 0 {
						t.Fatal("filtered request used incorrect credentials")
					}
					selector, err := labels.Parse(req.URL.Query().Get("labelSelector"))
					if err != nil || selector.Empty() {
						t.Fatalf("missing access constraint: %v", err)
					}
					result := &corev1.SecretList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "SecretList"}, Items: []corev1.Secret{}}
					for _, tenant := range []string{"tenant-a", "tenant-b", "system"} {
						secret := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "visible", Namespace: tenant + "-ns", Labels: map[string]string{capsulemeta.ManagedByCapsuleLabel: tenant}}}
						if tenant != "tenant-a" {
							secret.Labels["dontexistsignoreme"] = "true"
						}
						if selector.Matches(labels.Set(secret.Labels)) {
							result.Items = append(result.Items, secret)
						}
					}
					body, err := json.Marshal(result)
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, err
				})
				req := httptest.NewRequest(http.MethodGet, "https://proxy.example/api/v1/secrets", nil).WithContext(t.Context())
				req.Header.Set("Authorization", "Bearer verified-test-token")
				if mode == "conflicting-selector" {
					query := req.URL.Query()
					query.Set("labelSelector", capsulemeta.ManagedByCapsuleLabel+"=tenant-b")
					req.URL.RawQuery = query.Encode()
				}
				response := httptest.NewRecorder()
				f.handler.ServeHTTP(response, req)
				if response.Code != expected {
					t.Fatalf("status=%d want=%d: %s", response.Code, expected, response.Body.String())
				}
				if expected == http.StatusOK {
					var result corev1.SecretList
					if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					var actual []string
					for _, secret := range result.Items {
						actual = append(actual, secret.Namespace+"/"+secret.Name)
					}
					if len(actual) != len(expectedNames) || (len(actual) == 1 && actual[0] != expectedNames[0]) {
						t.Fatalf("secrets=%v want=%v", actual, expectedNames)
					}
				}
				if reviews != expectedReviews || f.reader.ownerLists.Load() != 1 || f.reviews.reviews.Load() != 3 {
					t.Fatalf("SAR=%d owner reads=%d TokenReviews=%d", reviews, f.reader.ownerLists.Load(), f.reviews.reviews.Load())
				}
				if expected == http.StatusInternalServerError && upstream != 0 {
					t.Fatal("failed ownership/authorization review reached upstream")
				}
			})
		}
	}
}
