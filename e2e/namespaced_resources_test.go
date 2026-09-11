//go:build e2e

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Namespaced resource access", Ordered, ContinueOnFailure, Label("namespaced"), func() {
	var f *accessFixture

	BeforeAll(func() {
		f = newAccessFixture()
		f.addTenant("a", capsulerbac.OwnerListSpec{
			f.owner(capsulerbac.UserOwner, f.subjects["owner"].username),
			f.owner(capsulerbac.UserOwner, f.subjects["coowner"].username),
			f.owner(capsulerbac.GroupOwner, f.id+"-team"),
			f.owner(capsulerbac.ServiceAccountOwner, f.subjects["serviceaccount"].username),
		}, "a1", "a2")
		f.addTenant("b", capsulerbac.OwnerListSpec{
			f.owner(capsulerbac.UserOwner, f.subjects["owner"].username),
			f.owner(capsulerbac.UserOwner, f.subjects["other"].username),
		}, "b1")
		f.addTenant("empty", capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, f.subjects["empty"].username)})
		role := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: f.id + "-configmap-reader"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get", "list"}}}}
		f.create(role)
		f.addTenant("limited", capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, f.subjects["limited"].username, role.Name)}, "limited")
		for _, resource := range accessResources() {
			f.seed(resource, "a", "a1", "a2")
			f.seed(resource, "b", "b1")
			f.seed(resource, "limited", "limited")
		}
		for _, subject := range []string{"owner", "coowner", "group", "serviceaccount"} {
			f.waitPermission(subject, "a1", "", "pods", true)
		}
		f.waitPermission("other", "b1", "", "pods", true)
		f.waitPermission("limited", "limited", "", "configmaps", true)
		f.waitPermission("limited", "limited", "", "secrets", false)
	})

	for _, resource := range accessResources() {
		Describe(resource.resource, func() {
			// This positive barrier prevents empty negative results caused by
			// unreconciled tenant state from being mistaken for correct isolation.
			BeforeAll(func() { f.expectList("owner", resource, "a1", "a2", "b1") })
			DescribeTable("lists exactly the resources visible to the subject", func(subject string, namespaces []string) {
				f.expectList(subject, resource, namespaces...)
				if len(namespaces) == 0 {
					Consistently(func() ([]string, error) { return f.list(subject, resource, "", f.listOptions()) }, time.Second, 200*time.Millisecond).Should(BeEmpty())
				}
			},
				Entry("owner of multiple tenants", "owner", []string{"a1", "a2", "b1"}),
				Entry("co-owner of one tenant", "coowner", []string{"a1", "a2"}),
				Entry("owner of the other tenant", "other", []string{"b1"}),
				Entry("group owner member", "group", []string{"a1", "a2"}),
				Entry("service account owner", "serviceaccount", []string{"a1", "a2"}),
				Entry("non-owner", "outsider", []string{}),
				Entry("user named like an owner group", "group-name-user", []string{}),
				Entry("same service account name in another namespace", "wrong-serviceaccount", []string{}),
				Entry("owner of a tenant with no namespaces", "empty", []string{}),
			)

			It("enforces the owner's resource-specific RBAC", func() {
				if resource.resource == "configmaps" {
					f.expectList("limited", resource, "limited")
				} else {
					f.expectList("limited", resource)
				}
			})

			It("intersects label and field selectors with tenant visibility", func() {
				options := f.listOptions()
				options.LabelSelector += "," + accessVariantLabel + "=sample"
				Eventually(func() ([]string, error) { return f.list("coowner", resource, "", options) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.ids([]string{"a1", "a2"}, "sample")))
				options = f.listOptions()
				options.FieldSelector = "metadata.name=alternate"
				Eventually(func() ([]string, error) { return f.list("coowner", resource, "", options) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.ids([]string{"a1", "a2"}, "alternate")))
				options.FieldSelector = "metadata.namespace=" + f.namespaces["b1"]
				Expect(f.list("coowner", resource, "", options)).To(BeEmpty())
				options = f.listOptions()
				options.LabelSelector += "," + capsulemeta.ManagedByCapsuleLabel + "=" + f.tenants["b"].Name
				Expect(f.list("coowner", resource, "", options)).To(BeEmpty())
			})

			It("allows a namespace-scoped list only where Kubernetes RBAC permits it", func() {
				Eventually(func() ([]string, error) { return f.list("coowner", resource, f.namespaces["a1"], f.listOptions()) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.ids([]string{"a1"}, "sample", "alternate")))
				for _, subject := range []string{"coowner", "outsider", "group-name-user", "wrong-serviceaccount"} {
					_, err := f.list(subject, resource, f.namespaces["b1"], f.listOptions())
					Expect(apierrors.IsForbidden(err)).To(BeTrue(), "%s must not list another tenant's %s: %v", subject, resource.resource, err)
				}
			})
		})
	}

	It("lists without a caller-supplied selector and preserves pagination isolation", func() {
		pods := accessResources()[0]
		Eventually(func() ([]string, error) { return f.list("coowner", pods, "", metav1.ListOptions{}) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.ids([]string{"a1", "a2"}, "sample", "alternate")))
		options := f.listOptions()
		options.Limit = 1
		var ids []string
		seen := map[string]bool{}
		for {
			page, err := f.subjects["coowner"].client.Resource(pods.gvr()).List(context.Background(), options)
			Expect(err).NotTo(HaveOccurred())
			for _, item := range page.Items {
				ids = append(ids, item.GetNamespace()+"/"+item.GetName())
			}
			options.Continue = page.GetContinue()
			if options.Continue == "" {
				break
			}
			Expect(seen[options.Continue]).To(BeFalse(), "pagination must advance")
			seen[options.Continue] = true
		}
		Expect(ids).To(ConsistOf(f.ids([]string{"a1", "a2"}, "sample", "alternate")))
	})

	It("removes a former owner's access after tenant reconciliation", func() {
		pods := accessResources()[0]
		f.expectList("coowner", pods, "a1", "a2")
		current := f.tenants["a"].DeepCopy()
		Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(current), current)).To(Succeed())
		owners := current.Spec.Owners[:0]
		for _, owner := range current.Spec.Owners {
			if owner.Name != f.subjects["coowner"].username {
				owners = append(owners, owner)
			}
		}
		current.Spec.Owners = owners
		Expect(k8sClient.Update(context.Background(), current)).To(Succeed())
		f.waitPermission("coowner", "a1", "", "pods", false)
		f.expectList("coowner", pods)
		f.expectList("group", pods, "a1", "a2")
	})
})
