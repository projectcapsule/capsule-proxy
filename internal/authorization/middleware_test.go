// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package authorization

import (
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

func hasResourceRule(rules []authorizationv1.ResourceRule, group, resource, verb string) bool {
	for _, r := range rules {
		var groupMatch, resourceMatch, verbMatch bool

		for _, g := range r.APIGroups {
			if g == group {
				groupMatch = true
			}
		}

		for _, res := range r.Resources {
			if res == resource {
				resourceMatch = true
			}
		}

		for _, v := range r.Verbs {
			if v == verb {
				verbMatch = true
			}
		}

		if groupMatch && resourceMatch && verbMatch {
			return true
		}
	}

	return false
}

func TestMutateAuthorization_SelfSubjectRulesReviewMerges(t *testing.T) {
	t.Parallel()

	// Rules resolved by the API server for the requester must be preserved.
	apiServerRule := authorizationv1.ResourceRule{
		APIGroups: []string{"apps"},
		Resources: []string{"deployments"},
		Verbs:     []string{"get", "list", "watch"},
	}

	review := &authorizationv1.SelfSubjectRulesReview{
		Status: authorizationv1.SubjectRulesReviewStatus{
			ResourceRules:    []authorizationv1.ResourceRule{apiServerRule},
			NonResourceRules: []authorizationv1.NonResourceRule{{Verbs: []string{"get"}, NonResourceURLs: []string{"/healthz"}}},
			Incomplete:       true,
		},
	}

	proxyTenants := []*tenant.ProxyTenant{
		{
			ClusterResources: []v1beta1.ClusterResource{
				{
					APIGroups:  []string{"storage.k8s.io"},
					Resources:  []string{"storageclasses"},
					Operations: []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationList},
				},
			},
		},
	}

	var obj runtime.Object = review

	if err := MutateAuthorization(true, proxyTenants, nil, &obj, schema.GroupVersionKind{Kind: "SelfSubjectRulesReview"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rules := review.Status.ResourceRules

	if !hasResourceRule(rules, "apps", "deployments", "list") {
		t.Errorf("expected the API server rules to be preserved, got %+v", rules)
	}

	if !hasResourceRule(rules, "", "namespaces", "list") {
		t.Errorf("expected injected namespaces/list rule, got %+v", rules)
	}

	if !hasResourceRule(rules, "storage.k8s.io", "storageclasses", "list") {
		t.Errorf("expected injected cluster resource rule, got %+v", rules)
	}

	if !review.Status.Incomplete {
		t.Errorf("expected Incomplete flag from the API server to be preserved")
	}

	if len(review.Status.NonResourceRules) != 1 {
		t.Errorf("expected NonResourceRules from the API server to be preserved, got %+v", review.Status.NonResourceRules)
	}
}

func TestMutateAuthorization_SelfSubjectRulesReviewWithoutClusterScoped(t *testing.T) {
	t.Parallel()

	review := &authorizationv1.SelfSubjectRulesReview{}

	var obj runtime.Object = review

	if err := MutateAuthorization(false, nil, nil, &obj, schema.GroupVersionKind{Kind: "SelfSubjectRulesReview"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !hasResourceRule(review.Status.ResourceRules, "", "namespaces", "list") {
		t.Errorf("expected injected namespaces/list rule, got %+v", review.Status.ResourceRules)
	}
}

func TestMutateAuthorization_SelfSubjectAccessReviewNamespacesList(t *testing.T) {
	t.Parallel()

	review := &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Resource: "namespaces",
				Verb:     "list",
			},
		},
		Status: authorizationv1.SubjectAccessReviewStatus{Denied: true},
	}

	var obj runtime.Object = review

	if err := MutateAuthorization(false, nil, nil, &obj, schema.GroupVersionKind{Kind: "SelfSubjectAccessReview"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !review.Status.Allowed {
		t.Errorf("expected namespaces/list to be allowed")
	}

	if review.Status.Denied {
		t.Errorf("expected Denied to be cleared when capsule-proxy grants access")
	}
}

func TestMutateAuthorization_SelfSubjectAccessReviewNonResourceAttributes(t *testing.T) {
	t.Parallel()

	// A review carrying NonResourceAttributes must not panic and must be left
	// untouched.
	review := &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			NonResourceAttributes: &authorizationv1.NonResourceAttributes{
				Path: "/healthz",
				Verb: "get",
			},
		},
		Status: authorizationv1.SubjectAccessReviewStatus{Allowed: true},
	}

	var obj runtime.Object = review

	if err := MutateAuthorization(true, nil, nil, &obj, schema.GroupVersionKind{Kind: "SelfSubjectAccessReview"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !review.Status.Allowed {
		t.Errorf("expected the API server verdict to be preserved for NonResourceAttributes reviews")
	}
}

func namespacedAccessReview(group, resource, verb, namespace string) *authorizationv1.SelfSubjectAccessReview {
	return &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Group:     group,
				Resource:  resource,
				Verb:      verb,
				Namespace: namespace,
			},
		},
		Status: authorizationv1.SubjectAccessReviewStatus{Denied: true},
	}
}

func TestMutateAuthorization_SelfSubjectAccessReviewNamespacedAllNamespaces(t *testing.T) {
	t.Parallel()

	// capsule-proxy serves `kubectl get pods -A` for tenant owners, so an
	// otherwise denied cluster-scoped list must be granted.
	proxyTenants := []*tenant.ProxyTenant{{}}
	namespaced := sets.New[string](NamespacedResourceKey("", "pods"))

	for _, verb := range []string{"list", "watch"} {
		review := namespacedAccessReview("", "pods", verb, "")

		var obj runtime.Object = review

		if err := MutateAuthorization(false, proxyTenants, namespaced, &obj, schema.GroupVersionKind{Kind: "SelfSubjectAccessReview"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !review.Status.Allowed || review.Status.Denied {
			t.Errorf("expected %s pods -A to be granted for a tenant owner, got %+v", verb, review.Status)
		}
	}
}

func TestMutateAuthorization_SelfSubjectAccessReviewNamespacedNotTenantOwner(t *testing.T) {
	t.Parallel()

	// Non tenant owners must keep the API server verdict untouched.
	namespaced := sets.New[string](NamespacedResourceKey("", "pods"))
	review := namespacedAccessReview("", "pods", "list", "")

	var obj runtime.Object = review

	if err := MutateAuthorization(false, nil, namespaced, &obj, schema.GroupVersionKind{Kind: "SelfSubjectAccessReview"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if review.Status.Allowed || !review.Status.Denied {
		t.Errorf("expected the API server verdict to be preserved for non tenant owners, got %+v", review.Status)
	}
}

func TestMutateAuthorization_SelfSubjectAccessReviewNamespacedScopedRequest(t *testing.T) {
	t.Parallel()

	// A namespace-scoped review is answered by the API server itself, so the
	// proxy must not override its verdict.
	proxyTenants := []*tenant.ProxyTenant{{}}
	namespaced := sets.New[string](NamespacedResourceKey("", "pods"))
	review := namespacedAccessReview("", "pods", "list", "tenant-ns")

	var obj runtime.Object = review

	if err := MutateAuthorization(false, proxyTenants, namespaced, &obj, schema.GroupVersionKind{Kind: "SelfSubjectAccessReview"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if review.Status.Allowed || !review.Status.Denied {
		t.Errorf("expected namespace-scoped reviews to be left untouched, got %+v", review.Status)
	}
}

func TestMutateAuthorization_SelfSubjectAccessReviewNonProxiedResource(t *testing.T) {
	t.Parallel()

	// A resource that capsule-proxy does not proxy as namespaced (e.g. a
	// cluster-scoped resource) must not be granted through this path.
	proxyTenants := []*tenant.ProxyTenant{{}}
	namespaced := sets.New[string](NamespacedResourceKey("", "pods"))
	review := namespacedAccessReview("", "nodes", "list", "")

	var obj runtime.Object = review

	if err := MutateAuthorization(false, proxyTenants, namespaced, &obj, schema.GroupVersionKind{Kind: "SelfSubjectAccessReview"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if review.Status.Allowed || !review.Status.Denied {
		t.Errorf("expected non-proxied resources to be left untouched, got %+v", review.Status)
	}
}
