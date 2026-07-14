// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"testing"

	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetUserNamespacesForResource(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := rbacv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	objects := []client.Object{
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "pod-reader", Namespace: "explicit"}, Rules: []rbacv1.PolicyRule{{
			Verbs: []string{"get", "list"}, APIGroups: []string{""}, Resources: []string{"pods"},
		}}},
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "wildcard-reader", Namespace: "wildcard"}, Rules: []rbacv1.PolicyRule{{
			Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"},
		}}},
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "get-only", Namespace: "denied"}, Rules: []rbacv1.PolicyRule{{
			Verbs: []string{"get"}, APIGroups: []string{""}, Resources: []string{"pods"},
		}}},
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "named", Namespace: "named"}, Rules: []rbacv1.PolicyRule{{
			Verbs: []string{"list"}, APIGroups: []string{""}, Resources: []string{"pods"}, ResourceNames: []string{"one"},
		}}},
	}
	for _, binding := range []*rbacv1.RoleBinding{
		roleBinding("explicit", "pod-reader", rbacv1.Subject{Kind: "User", Name: "alice"}),
		roleBinding("wildcard", "wildcard-reader", rbacv1.Subject{Kind: "Group", Name: "developers"}),
		roleBinding("denied", "get-only", rbacv1.Subject{Kind: "User", Name: "alice"}),
		roleBinding("named", "named", rbacv1.Subject{Kind: "User", Name: "alice"}),
		unreflectedRoleBinding("unlabelled", "pod-reader", rbacv1.Subject{Kind: "User", Name: "alice"}),
	} {
		objects = append(objects, binding)
	}

	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).
		WithIndex(&rbacv1.RoleBinding{}, reflectionSubjectIndex, func(obj client.Object) []string {
			values, _ := ReflectionRoleBindingsIndexFunc(obj)

			return values
		}).Build()
	reflector := &RoleBindingReflector{reader: reader, results: map[string]cachedReflectionResult{}}

	namespaces, err := reflector.GetUserNamespacesForResource(
		context.Background(), "alice", []string{"developers"}, "list", "", "pods",
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(namespaces) != 2 || namespaces[0] != "explicit" || namespaces[1] != "wildcard" {
		t.Fatalf("expected explicit and wildcard namespaces, got %v", namespaces)
	}
}

func TestPolicyRuleAllows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rule rbacv1.PolicyRule
		want bool
	}{
		{name: "exact", rule: rbacv1.PolicyRule{Verbs: []string{"list"}, APIGroups: []string{"apps"}, Resources: []string{"deployments"}}, want: true},
		{name: "wildcards", rule: rbacv1.PolicyRule{Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}}, want: true},
		{name: "wrong verb", rule: rbacv1.PolicyRule{Verbs: []string{"get"}, APIGroups: []string{"apps"}, Resources: []string{"deployments"}}},
		{name: "wrong group", rule: rbacv1.PolicyRule{Verbs: []string{"list"}, APIGroups: []string{"batch"}, Resources: []string{"deployments"}}},
		{name: "wrong resource", rule: rbacv1.PolicyRule{Verbs: []string{"list"}, APIGroups: []string{"apps"}, Resources: []string{"statefulsets"}}},
		{name: "resource names", rule: rbacv1.PolicyRule{Verbs: []string{"list"}, APIGroups: []string{"apps"}, Resources: []string{"deployments"}, ResourceNames: []string{"one"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := policyRuleAllows(tt.rule, "list", "apps", "deployments"); got != tt.want {
				t.Fatalf("policyRuleAllows() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReflectedTenantNamePriority(t *testing.T) {
	t.Parallel()

	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Labels: map[string]string{
			capsulemeta.NewTenantLabel: "new-label-tenant",
			capsulemeta.TenantLabel:    "legacy-label-tenant",
		},
		OwnerReferences: []metav1.OwnerReference{{Kind: "Tenant", Name: "owner-tenant"}},
	}}

	if got := reflectedTenantName(namespace); got != "new-label-tenant" {
		t.Fatalf("expected new tenant label, got %q", got)
	}

	delete(namespace.Labels, capsulemeta.NewTenantLabel)
	if got := reflectedTenantName(namespace); got != "legacy-label-tenant" {
		t.Fatalf("expected legacy tenant label, got %q", got)
	}

	delete(namespace.Labels, capsulemeta.TenantLabel)
	if got := reflectedTenantName(namespace); got != "owner-tenant" {
		t.Fatalf("expected tenant owner reference, got %q", got)
	}
}

func roleBinding(namespace, role string, subject rbacv1.Subject) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      role + "-binding",
			Namespace: namespace,
			Labels:    map[string]string{RoleBindingReflectionLabel: "true"},
		},
		RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role},
		Subjects: []rbacv1.Subject{subject},
	}
}

func unreflectedRoleBinding(namespace, role string, subject rbacv1.Subject) *rbacv1.RoleBinding {
	binding := roleBinding(namespace, role, subject)
	binding.Labels = nil

	return binding
}

func TestRoleBindingIndexesScopeOnlyResourceReflection(t *testing.T) {
	t.Parallel()

	subject := rbacv1.Subject{Kind: "User", Name: "alice"}

	indexed, err := OwnerRoleBindingsIndexFunc(roleBinding("tenant-a", "reader", subject))
	if err != nil {
		t.Fatal(err)
	}
	if len(indexed) != 1 || indexed[0] != "User-alice" {
		t.Fatalf("expected labelled binding to be indexed, got %v", indexed)
	}

	indexed, err = OwnerRoleBindingsIndexFunc(unreflectedRoleBinding("tenant-a", "reader", subject))
	if err != nil {
		t.Fatal(err)
	}
	if len(indexed) != 1 || indexed[0] != "User-alice" {
		t.Fatalf("expected unlabelled binding in the namespace index, got %v", indexed)
	}

	indexed, err = ReflectionRoleBindingsIndexFunc(unreflectedRoleBinding("tenant-a", "reader", subject))
	if err != nil {
		t.Fatal(err)
	}
	if len(indexed) != 0 {
		t.Fatalf("expected unlabelled binding not to be in the resource reflection index, got %v", indexed)
	}
}
