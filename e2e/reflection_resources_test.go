//go:build e2e

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/controllers"
)

var _ = Describe("Reflected resource access", Ordered, ContinueOnFailure, Label("reflection"), func() {
	var f *accessFixture
	var subjects map[string][]rbacv1.Subject
	var bindings []*rbacv1.RoleBinding
	var roles []client.Object
	resources := []accessResource{accessResources()[0], accessResources()[1], accessResources()[2], accessResources()[6], accessResources()[9]}

	BeforeAll(func() {
		f = newAccessFixture()
		f.addTenant("reflected", capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, f.subjects["owner"].username)}, "r1", "r2")
		f.addTenant("private", capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, f.subjects["other"].username)}, "private")
		for _, resource := range resources {
			f.seed(resource, "reflected", "r1", "r2")
			f.seed(resource, "private", "private")
			f.expectList("owner", resource, "r1", "r2")
			f.expectList("other", resource, "private")
		}
		subjects = map[string][]rbacv1.Subject{
			"user":           {reflectionSubject(rbacv1.UserKind, f.subjects["coowner"].username, "")},
			"group":          {reflectionSubject(rbacv1.GroupKind, f.id+"-team", "")},
			"serviceaccount": {reflectionSubject(rbacv1.ServiceAccountKind, "reader", f.namespaces["identities"])},
		}
		subjects["multiple"] = append(append(append([]rbacv1.Subject{}, subjects["user"]...), subjects["group"]...), subjects["serviceaccount"]...)
	})

	BeforeEach(func() { bindings, roles = nil, nil })

	bind := func(resource accessResource, roleKind, label string, rules []rbacv1.PolicyRule, boundSubjects []rbacv1.Subject) (*rbacv1.RoleBinding, client.Object) {
		name := f.id + "-" + rand.String(8)
		var role client.Object
		if roleKind == "Role" {
			role = &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: f.namespaces["r1"]}, Rules: rules}
		} else {
			role = &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: name}, Rules: rules}
		}
		Expect(k8sClient.Create(context.Background(), role)).To(Succeed())
		roles = append(roles, role)
		binding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: f.namespaces["r1"]}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: roleKind, Name: name}, Subjects: boundSubjects}
		if label != "" {
			binding.Labels = map[string]string{controllers.RoleBindingReflectionLabel: label}
		}
		Expect(k8sClient.Create(context.Background(), binding)).To(Succeed())
		bindings = append(bindings, binding)
		return binding, role
	}

	waitNamespace := func(subject string, visible bool) {
		Eventually(func(g Gomega) {
			list, err := f.subjects[subject].client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}).List(context.Background(), f.listOptions())
			g.Expect(err).NotTo(HaveOccurred())
			names := []string{}
			for _, item := range list.Items {
				names = append(names, item.GetName())
			}
			if visible {
				g.Expect(names).To(ContainElement(f.namespaces["r1"]))
			} else {
				g.Expect(names).NotTo(ContainElement(f.namespaces["r1"]))
			}
		}, accessTimeout, defaultPollInterval).Should(Succeed())
	}

	AfterEach(func() {
		for _, binding := range bindings {
			Expect(client.IgnoreNotFound(k8sClient.Delete(context.Background(), binding))).To(Succeed())
		}
		for _, role := range roles {
			Expect(client.IgnoreNotFound(k8sClient.Delete(context.Background(), role))).To(Succeed())
		}
		if f == nil || len(bindings) == 0 {
			return
		}
		// Wait for informer removal, including replacement subjects, before
		// starting another case. Multiple bindings are removed together.
		for _, subject := range []string{"coowner", "group", "serviceaccount", "outsider"} {
			waitNamespace(subject, false)
		}
	})

	for _, resource := range resources {
		Describe(resource.resource, func() {
			DescribeTable("reflects resource permissions for different subjects and role kinds", func(roleKind, subjectKind string) {
				rules := []rbacv1.PolicyRule{{APIGroups: []string{resource.group}, Resources: []string{resource.resource}, Verbs: []string{"get", "list"}}}
				bind(resource, roleKind, "true", rules, subjects[subjectKind])
				allowed := map[string]bool{}
				switch subjectKind {
				case "user":
					allowed["coowner"] = true
				case "group":
					allowed["group"] = true
				case "serviceaccount":
					allowed["serviceaccount"] = true
				case "multiple":
					allowed = map[string]bool{"coowner": true, "group": true, "serviceaccount": true}
				}
				for subject := range allowed {
					f.waitPermission(subject, "r1", resource.group, resource.resource, true)
					waitNamespace(subject, true)
					// Reflection currently expands a grant to the tenant selector,
					// including sibling namespaces. Namespace-scoped calls still use RBAC.
					f.expectList(subject, resource, "r1", "r2")
					Eventually(func() ([]string, error) { return f.list(subject, resource, f.namespaces["r1"], f.listOptions()) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.ids([]string{"r1"}, "sample", "alternate")))
					_, err := f.list(subject, resource, f.namespaces["r2"], f.listOptions())
					Expect(apierrors.IsForbidden(err)).To(BeTrue(), "sibling namespace RBAC must remain independent: %v", err)
					options := f.listOptions()
					options.FieldSelector = "metadata.name=sample"
					Expect(f.list(subject, resource, "", options)).To(ConsistOf(f.ids([]string{"r1", "r2"}, "sample")))
				}
				for _, subject := range []string{"coowner", "group", "serviceaccount", "outsider", "group-name-user", "wrong-serviceaccount"} {
					if !allowed[subject] {
						f.expectList(subject, resource)
					}
				}
				for _, other := range resources {
					if other.resource == resource.resource {
						continue
					}
					for subject := range allowed {
						f.expectList(subject, other)
					}
				}
			},
				Entry("User with a Role", "Role", "user"),
				Entry("Group with a Role", "Role", "group"),
				Entry("ServiceAccount with a Role", "Role", "serviceaccount"),
				Entry("User with a ClusterRole", "ClusterRole", "user"),
				Entry("Group with a ClusterRole", "ClusterRole", "group"),
				Entry("ServiceAccount with a ClusterRole", "ClusterRole", "serviceaccount"),
				Entry("multiple subject kinds in one binding", "ClusterRole", "multiple"),
			)

			DescribeTable("does not reflect rules without an unrestricted list grant", func(label, mismatch string) {
				rule := rbacv1.PolicyRule{APIGroups: []string{resource.group}, Resources: []string{resource.resource}, Verbs: []string{"list"}}
				switch mismatch {
				case "get":
					rule.Verbs = []string{"get"}
				case "watch":
					rule.Verbs = []string{"watch"}
				case "resource":
					rule.Resources = []string{"unrelatedresources"}
				case "group":
					rule.APIGroups = []string{"unrelated.example"}
				case "names":
					rule.ResourceNames = []string{"sample"}
				}
				bind(resource, "Role", label, []rbacv1.PolicyRule{rule}, subjects["multiple"])
				for _, subject := range []string{"coowner", "group", "serviceaccount"} {
					waitNamespace(subject, true)
					f.expectList(subject, resource)
					Consistently(func() ([]string, error) { return f.list(subject, resource, "", f.listOptions()) }, time.Second, 200*time.Millisecond).Should(BeEmpty())
				}
			},
				Entry("unlabelled binding", "", ""),
				Entry("disabled reflection label", "false", ""),
				Entry("label values are case sensitive", "True", ""),
				Entry("get does not imply list", "true", "get"),
				Entry("watch does not imply list", "true", "watch"),
				Entry("different resource", "true", "resource"),
				Entry("different API group", "true", "group"),
				Entry("resourceNames cannot authorize an unfiltered list", "true", "names"),
			)

			It("honors wildcard API groups, resources and verbs", func() {
				bind(resource, "ClusterRole", "true", []rbacv1.PolicyRule{{APIGroups: []string{"*"}, Resources: []string{"*"}, Verbs: []string{"*"}}}, subjects["multiple"])
				for _, subject := range []string{"coowner", "group", "serviceaccount"} {
					f.expectList(subject, resource, "r1", "r2")
				}
			})
		})
	}

	It("deduplicates multiple bindings and unions ownership with reflection", func() {
		resource := resources[0]
		rules := []rbacv1.PolicyRule{{APIGroups: []string{resource.group}, Resources: []string{resource.resource}, Verbs: []string{"list"}}}
		mixed := append(append([]rbacv1.Subject{}, subjects["user"]...), reflectionSubject(rbacv1.UserKind, f.subjects["other"].username, ""))
		bind(resource, "Role", "true", rules, mixed)
		bind(resource, "ClusterRole", "true", rules, mixed)
		f.expectList("coowner", resource, "r1", "r2")
		f.expectList("other", resource, "r1", "r2", "private")
	})

	DescribeTable("invalidates cached results after RBAC changes", func(change string) {
		resource := resources[1]
		rules := []rbacv1.PolicyRule{{APIGroups: []string{resource.group}, Resources: []string{resource.resource}, Verbs: []string{"list"}}}
		roleKind := "Role"
		if change == "clusterrole rules" || change == "delete clusterrole" {
			roleKind = "ClusterRole"
		}
		binding, role := bind(resource, roleKind, "true", rules, subjects["multiple"])
		for _, subject := range []string{"coowner", "group", "serviceaccount"} {
			f.expectList(subject, resource, "r1", "r2")
		}
		switch change {
		case "subjects":
			Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(binding), binding)).To(Succeed())
			binding.Subjects = []rbacv1.Subject{reflectionSubject(rbacv1.UserKind, f.subjects["outsider"].username, "")}
			Expect(k8sClient.Update(context.Background(), binding)).To(Succeed())
		case "label":
			Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(binding), binding)).To(Succeed())
			delete(binding.Labels, controllers.RoleBindingReflectionLabel)
			Expect(k8sClient.Update(context.Background(), binding)).To(Succeed())
		case "role rules", "clusterrole rules":
			Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(role), role)).To(Succeed())
			switch role := role.(type) {
			case *rbacv1.Role:
				role.Rules = nil
			case *rbacv1.ClusterRole:
				role.Rules = nil
			}
			Expect(k8sClient.Update(context.Background(), role)).To(Succeed())
		case "delete binding":
			Expect(k8sClient.Delete(context.Background(), binding)).To(Succeed())
		case "delete role", "delete clusterrole":
			Expect(k8sClient.Delete(context.Background(), role)).To(Succeed())
		}
		for _, subject := range []string{"coowner", "group", "serviceaccount"} {
			f.expectList(subject, resource)
			Consistently(func() ([]string, error) { return f.list(subject, resource, "", f.listOptions()) }, time.Second, 200*time.Millisecond).Should(BeEmpty())
		}
		if change == "subjects" {
			f.expectList("outsider", resource, "r1", "r2")
		}
		f.expectList("owner", resource, "r1", "r2")
	},
		Entry("subject replacement", "subjects"),
		Entry("reflection label removal", "label"),
		Entry("Role rule removal", "role rules"),
		Entry("ClusterRole rule removal", "clusterrole rules"),
		Entry("RoleBinding deletion", "delete binding"),
		Entry("Role deletion leaves a dangling reference", "delete role"),
		Entry("ClusterRole deletion leaves a dangling reference", "delete clusterrole"),
	)

	DescribeTable("grants access after a previously cached denial", func(change string) {
		resource := resources[1]
		roleKind, label := "Role", "true"
		rules := []rbacv1.PolicyRule{{APIGroups: []string{resource.group}, Resources: []string{resource.resource}, Verbs: []string{"list"}}}
		boundSubjects := subjects["multiple"]
		switch change {
		case "label":
			label = "false"
		case "role", "clusterrole":
			rules[0].Verbs = []string{"get"}
			if change == "clusterrole" {
				roleKind = "ClusterRole"
			}
		case "subjects":
			boundSubjects = []rbacv1.Subject{reflectionSubject(rbacv1.UserKind, f.subjects["outsider"].username, "")}
		}
		binding, role := bind(resource, roleKind, label, rules, boundSubjects)
		if change == "subjects" {
			waitNamespace("outsider", true)
		} else {
			waitNamespace("coowner", true)
		}
		for _, subject := range []string{"coowner", "group", "serviceaccount"} {
			f.expectList(subject, resource)
		}

		switch change {
		case "label", "subjects":
			Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(binding), binding)).To(Succeed())
			binding.Labels[controllers.RoleBindingReflectionLabel] = "true"
			binding.Subjects = subjects["multiple"]
			Expect(k8sClient.Update(context.Background(), binding)).To(Succeed())
		case "role", "clusterrole":
			Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(role), role)).To(Succeed())
			switch role := role.(type) {
			case *rbacv1.Role:
				role.Rules[0].Verbs = []string{"get", "list"}
			case *rbacv1.ClusterRole:
				role.Rules[0].Verbs = []string{"get", "list"}
			}
			Expect(k8sClient.Update(context.Background(), role)).To(Succeed())
		}
		for _, subject := range []string{"coowner", "group", "serviceaccount"} {
			f.expectList(subject, resource, "r1", "r2")
		}
		f.expectList("outsider", resource)
	},
		Entry("enabling the reflection label", "label"),
		Entry("adding list to a Role", "role"),
		Entry("adding list to a ClusterRole", "clusterrole"),
		Entry("adding subjects to a binding", "subjects"),
	)
})
