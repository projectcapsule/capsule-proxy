// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Namespace create-if-missing clients", func() {
	const (
		aliceTenantName = "create-client-alice"
		bobTenantName   = "create-client-bob"
	)

	forceTenantPrefix := true
	aliceTenant := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: aliceTenantName, Labels: e2eLabels()},
		Spec: capsulev1beta2.TenantSpec{
			ForceTenantPrefix: &forceTenantPrefix,
			Owners: capsulerbac.OwnerListSpec{{
				CoreOwnerSpec: capsulerbac.CoreOwnerSpec{
					UserSpec: capsulerbac.UserSpec{Kind: capsulerbac.UserOwner, Name: "alice"},
				},
			}},
		},
	}
	bobTenant := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: bobTenantName, Labels: e2eLabels()},
		Spec: capsulev1beta2.TenantSpec{
			ForceTenantPrefix: &forceTenantPrefix,
			Owners: capsulerbac.OwnerListSpec{{
				CoreOwnerSpec: capsulerbac.CoreOwnerSpec{
					UserSpec: capsulerbac.UserSpec{Kind: capsulerbac.UserOwner, Name: "bob"},
				},
			}},
		},
	}

	var aliceClient, bobClient *kubernetes.Clientset

	BeforeEach(func() {
		var err error
		aliceClient, err = loadKubeConfig("alice")
		Expect(err).NotTo(HaveOccurred())
		bobClient, err = loadKubeConfig("bob")
		Expect(err).NotTo(HaveOccurred())

		for _, tenant := range []*capsulev1beta2.Tenant{aliceTenant, bobTenant} {
			tenant := tenant
			Eventually(func() error {
				tenant.ResourceVersion = ""

				return k8sClient.Create(context.Background(), tenant)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				current := &capsulev1beta2.Tenant{}
				g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: tenant.Name}, current)).To(Succeed())
				g.Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))

				var ownerClusterRoles []string
				for _, owner := range current.Status.Owners {
					if owner.Kind == tenant.Spec.Owners[0].Kind && owner.Name == tenant.Spec.Owners[0].Name {
						ownerClusterRoles = owner.ClusterRoles

						break
					}
				}
				g.Expect(ownerClusterRoles).NotTo(BeEmpty(), "the e2e case must exercise owner RoleBinding readiness")
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})

	JustAfterEach(func() {
		for _, namespace := range []string{
			aliceTenantName + "-allowed",
			bobTenantName + "-existing",
			bobTenantName + "-denied",
		} {
			namespace := namespace
			Eventually(func() error {
				return client.IgnoreNotFound(k8sClient.Delete(context.Background(), &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: namespace},
				}))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), client.ObjectKey{Name: namespace}, &corev1.Namespace{})

				return apierrors.IsNotFound(err)
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue(), "namespace %q should be deleted", namespace)
		}

		for _, tenant := range []*capsulev1beta2.Tenant{aliceTenant, bobTenant} {
			tenant := tenant
			Eventually(func() error {
				return client.IgnoreNotFound(k8sClient.Delete(context.Background(), &capsulev1beta2.Tenant{
					ObjectMeta: metav1.ObjectMeta{Name: tenant.Name},
				}))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})

	It("returns NotFound only when the target namespace is missing", func() {
		_, err := aliceClient.CoreV1().ServiceAccounts(aliceTenantName+"-missing").Get(
			context.Background(), "release", metav1.GetOptions{},
		)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "a named resource lookup in a missing namespace should receive 404: %v", err)

		_, err = bobClient.CoreV1().ServiceAccounts(aliceTenantName+"-missing").Get(
			context.Background(), "release", metav1.GetOptions{},
		)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "the proxy must not decide whether the caller may create the namespace: %v", err)

		existingNamespace, err := bobClient.CoreV1().Namespaces().Create(
			context.Background(),
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: bobTenantName + "-existing"}},
			metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())

		_, err = aliceClient.CoreV1().ServiceAccounts(existingNamespace.Name).Get(
			context.Background(), "release", metav1.GetOptions{},
		)
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "an existing unauthorized namespace must preserve the upstream 403: %v", err)

		_, err = bobClient.CoreV1().Secrets(metav1.NamespaceSystem).Get(
			context.Background(), "missing", metav1.GetOptions{},
		)
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "system namespaces must remain outside the masking boundary: %v", err)
	})

	It("delegates namespace creation authorization to Capsule", func() {
		allowedName := aliceTenantName + "-allowed"
		created, err := aliceClient.CoreV1().Namespaces().Create(
			context.Background(),
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: allowedName}},
			metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(created.Name).To(Equal(allowedName))
		Expect([]string{
			created.Labels[capsulemeta.NewTenantLabel],
			created.Labels[capsulemeta.TenantLabel],
		}).To(ContainElement(aliceTenantName), "Capsule admission should assign the namespace to Alice's Tenant")

		deniedName := bobTenantName + "-denied"
		_, err = aliceClient.CoreV1().Namespaces().Create(
			context.Background(),
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: deniedName}},
			metav1.CreateOptions{},
		)
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "Capsule admission must deny cross-tenant namespace creation: %v", err)

		err = k8sClient.Get(context.Background(), client.ObjectKey{Name: deniedName}, &corev1.Namespace{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "a denied namespace must not be persisted: %v", err)
	})
})
