// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/projectcapsule/capsule-proxy/internal/controllers"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

type namespaceReviewReader struct {
	client.Reader
	reads int
	err   error
}

func TestNamespaceGetSelfReviewConcurrentSubjects(t *testing.T) {
	f := newGlobalAccessFixture(t, true, true, true)
	f.enableAuthorization(t)
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			allowed := i%2 == 0
			selected := "tenant-a-ns"
			if allowed {
				selected = "tenant-b-ns"
			}
			grants := []*tenant.ProxyTenant{{ClusterResources: globalNamespaceGrant(selected)}}
			body := []byte(`{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","spec":{"resourceAttributes":{"version":"v1","resource":"namespaces","verb":"get","name":"tenant-b-ns"}}}`)
			result, err := f.filter.mutateAuthorizationBody(t.Context(), body, "application/json", grants, true)
			if err != nil {
				t.Error(err)
				return
			}
			var review authorizationv1.SelfSubjectAccessReview
			if err := json.Unmarshal(result, &review); err != nil || review.Status.Allowed != allowed {
				t.Errorf("subject %d: status=%+v want allowed=%t, err=%v", i, review.Status, allowed, err)
			}
		})
	}
	wg.Wait()
}

func (r *namespaceReviewReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*corev1.Namespace); ok {
		r.reads++
		if r.err != nil {
			return r.err
		}
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}

func TestCapsuleNamespaceGetSelfReviews(t *testing.T) {
	isolateCapsuleIdentities(t)
	controllers.CapsuleUsers.Insert("external")
	for _, mediaType := range []string{"application/json", "application/vnd.kubernetes.protobuf"} {
		for _, mode := range []string{"selected", "unowned-selected", "excluded", "unowned-excluded", "native-allowed-excluded", "owner", "owner-gate-disabled", "reflected", "wrong-reflection-subject", "gate-disabled", "omitted-version", "wildcard-version", "wrong-version", "subresource", "namespace-attribute", "list", "unnamed", "mutation", "non-resource", "reader-error"} {
			t.Run(mediaType+"/"+mode, func(t *testing.T) {
				f := newGlobalAccessFixture(t, true, true, mode != "gate-disabled" && mode != "owner-gate-disabled")
				reader := &namespaceReviewReader{Reader: f.client}
				f.filter.reader = reader
				name := "tenant-b-ns"
				want, reads := true, 1
				switch mode {
				case "unowned-selected":
					name = "kube-system"
				case "excluded", "native-allowed-excluded", "owner", "owner-gate-disabled", "reflected", "wrong-reflection-subject":
					name = "tenant-a-ns"
				case "unowned-excluded":
					name = "kube-system"
				}
				selectedName := "tenant-b-ns"
				if mode == "unowned-selected" {
					selectedName = name
				}
				f.grant(t, capsulerbac.UserOwner, "external", globalNamespaceGrant(selectedName))
				if mode == "owner" || mode == "owner-gate-disabled" {
					tenant := &capsulev1beta2.Tenant{}
					if err := f.client.Get(t.Context(), client.ObjectKey{Name: "tenant-a"}, tenant); err != nil {
						t.Fatal(err)
					}
					tenant.Status.Owners = capsulerbac.OwnerStatusListSpec{{UserSpec: capsulerbac.UserSpec{Kind: capsulerbac.UserOwner, Name: "external"}}}
					if err := f.client.Update(t.Context(), tenant); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "reflected" || mode == "wrong-reflection-subject" {
					subject := "external-team"
					if mode == "wrong-reflection-subject" {
						subject = "other-team"
					}
					binding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "namespace-reader", Namespace: name, Labels: map[string]string{controllers.RoleBindingReflectionLabel: "true"}}, Subjects: []rbacv1.Subject{{Kind: "Group", APIGroup: rbacv1.GroupName, Name: subject}}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "reader"}}
					role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "reader", Namespace: name}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"list"}}}}
					cache := &globalReflectionCache{builder: fake.NewClientBuilder().WithScheme(f.client.Scheme()).WithObjects(binding, role)}
					reflector, err := controllers.NewRoleBindingReflector(t.Context(), cache)
					if err != nil {
						t.Fatal(err)
					}
					cache.reader = cache.builder.Build()
					f.filter.roleBindingsReflector = reflector
				}
				attributes := &authorizationv1.ResourceAttributes{Resource: "namespaces", Version: "v1", Verb: "get", Name: name}
				switch mode {
				case "excluded", "unowned-excluded", "native-allowed-excluded", "wrong-reflection-subject", "gate-disabled":
					want = false
				case "omitted-version":
					attributes.Version = ""
				case "wildcard-version":
					attributes.Version = "*"
				case "wrong-version":
					attributes.Version, want, reads = "v99", false, 0
				case "subresource":
					attributes.Subresource, want, reads = "status", false, 0
				case "namespace-attribute":
					attributes.Namespace, want, reads = "tenant-a-ns", false, 0
				case "list":
					attributes.Verb, attributes.Name, reads = "list", "", 0
				case "unnamed":
					attributes.Name, reads = "", 0
				case "mutation":
					attributes.Verb, want, reads = "delete", false, 0
				case "non-resource":
					attributes, want, reads = nil, false, 0
				case "reader-error":
					reader.err, want = errors.New("namespace lookup failed"), false
				}
				f.routes()
				f.enableAuthorization(t)
				review := &authorizationv1.SelfSubjectAccessReview{TypeMeta: metav1.TypeMeta{APIVersion: "authorization.k8s.io/v1", Kind: "SelfSubjectAccessReview"}, Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: attributes}, Status: authorizationv1.SubjectAccessReviewStatus{Denied: true, Reason: "native RBAC"}}
				if mode == "native-allowed-excluded" || mode == "reader-error" {
					review.Status.Allowed, review.Status.Denied = true, false
				}
				var body []byte
				var err error
				if mediaType == "application/json" {
					body, err = json.Marshal(review)
				} else {
					body, err = runtime.Encode(f.filter.protoEncoder, review)
				}
				if err != nil {
					t.Fatal(err)
				}
				f.filter.reverseProxy.Transport = promotionTransport(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusCreated, Header: http.Header{"Content-Type": {mediaType}, "X-Upstream": {"preserved"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
				})
				r := httptest.NewRequest(http.MethodPost, "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", nil)
				r.Header.Set("Authorization", "Bearer caller-token")
				response := httptest.NewRecorder()
				f.handler.ServeHTTP(response, r)
				if response.Code != http.StatusCreated || response.Header().Get("Content-Type") != mediaType || response.Header().Get("X-Upstream") != "preserved" {
					t.Fatalf("response changed: %d %v %s", response.Code, response.Header(), response.Body.String())
				}
				obj, _, err := f.filter.universalDecoder.Decode(response.Body.Bytes(), nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				status := obj.(*authorizationv1.SelfSubjectAccessReview).Status
				if status.Allowed != want {
					t.Errorf("status=%+v, want allowed=%t", status, want)
				}
				if mode == "reader-error" && status.EvaluationError == "" {
					t.Error("lookup failure must report an evaluation error")
				}
				if reader.reads != reads || f.writer.tokens.Load() != 1 || f.writer.reviews.Load() != 0 {
					t.Errorf("namespace reads=%d want=%d, token reviews=%d want=1, impersonation reviews=%d want=0", reader.reads, reads, f.writer.tokens.Load(), f.writer.reviews.Load())
				}
			})
		}
	}
}
