// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
)

var _ = Describe("observedGeneration is tracked in status", Ordered, Label("observedGeneration"), func() {
	Context("GlobalProxySettings (cluster-scoped)", func() {
		var gps *v1beta1.GlobalProxySettings

		JustBeforeEach(func() {
			gps = &v1beta1.GlobalProxySettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "e2e-observed-generation-gps",
					Labels: e2eLabels(),
				},
				Spec: v1beta1.GlobalProxySettingsSpec{
					Rules: []v1beta1.GlobalSubjectSpec{
						{
							Subjects: []v1beta1.GlobalSubject{
								{Kind: "User", Name: "e2e-observed-gen-user"},
							},
						},
					},
				},
			}

			Eventually(func() error {
				return k8sClient.Create(context.TODO(), gps)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		JustAfterEach(func() {
			Eventually(func() error {
				return cleanResources([]client.Object{&v1beta1.GlobalProxySettings{}}, e2eSelector())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		It("sets observedGeneration after initial reconciliation", func() {
			Eventually(func(g Gomega) {
				current := &v1beta1.GlobalProxySettings{}
				g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: gps.GetName()}, current)).To(Succeed())

				g.Expect(current.Status.ObservedGeneration).To(
					Equal(current.GetGeneration()),
					"expected status.observedGeneration (%d) to equal metadata.generation (%d)",
					current.Status.ObservedGeneration,
					current.GetGeneration(),
				)

				readyCond := current.Status.Conditions.GetConditionByType(capmeta.ReadyCondition)
				g.Expect(readyCond).NotTo(BeNil(), "expected Ready condition to be set")
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(readyCond.ObservedGeneration).To(Equal(current.GetGeneration()))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		It("updates observedGeneration after a spec change", func() {
			By("waiting for observedGeneration to be set initially", func() {
				Eventually(func(g Gomega) {
					current := &v1beta1.GlobalProxySettings{}
					g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: gps.GetName()}, current)).To(Succeed())
					g.Expect(current.Status.ObservedGeneration).To(BeNumerically(">=", int64(1)))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})

			By("mutating the spec to increment metadata.generation", func() {
				Eventually(func() error {
					current := &v1beta1.GlobalProxySettings{}
					if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: gps.GetName()}, current); err != nil {
						return err
					}

					current.Spec.Rules[0].Subjects = append(current.Spec.Rules[0].Subjects, v1beta1.GlobalSubject{
						Kind: "User",
						Name: "e2e-observed-gen-user-2",
					})

					return k8sClient.Update(context.TODO(), current)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})

			By("verifying observedGeneration matches the updated generation", func() {
				Eventually(func(g Gomega) {
					current := &v1beta1.GlobalProxySettings{}
					g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: gps.GetName()}, current)).To(Succeed())

					g.Expect(current.Status.ObservedGeneration).To(
						Equal(current.GetGeneration()),
						"expected status.observedGeneration (%d) to match updated metadata.generation (%d)",
						current.Status.ObservedGeneration,
						current.GetGeneration(),
					)

					readyCond := current.Status.Conditions.GetConditionByType(capmeta.ReadyCondition)
					g.Expect(readyCond).NotTo(BeNil(), "expected Ready condition to be set")
					g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(readyCond.ObservedGeneration).To(Equal(current.GetGeneration()))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})
		})
	})

	Context("ProxySetting (namespace-scoped)", func() {
		const testNamespace = "e2e-observed-gen-ns"

		var ns *corev1.Namespace
		var ps *v1beta1.ProxySetting

		JustBeforeEach(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   testNamespace,
					Labels: e2eLabels(),
				},
			}

			ps = &v1beta1.ProxySetting{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-observed-generation-ps",
					Namespace: testNamespace,
					Labels:    e2eLabels(),
				},
				Spec: v1beta1.ProxySettingSpec{
					Subjects: []v1beta1.OwnerSpec{
						{
							Kind: "User",
							Name: "e2e-observed-gen-user",
						},
					},
				},
			}

			Eventually(func() error {
				err := k8sClient.Create(context.TODO(), ns)
				if err != nil {
					// If namespace already exists, that's fine
					if client.IgnoreAlreadyExists(err) != nil {
						return err
					}
				}
				return nil
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			Eventually(func() error {
				return k8sClient.Create(context.TODO(), ps)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		JustAfterEach(func() {
			Eventually(func() error {
				return k8sClient.DeleteAllOf(context.TODO(), &v1beta1.ProxySetting{},
					client.InNamespace(testNamespace),
					client.MatchingLabels(e2eLabels()))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			// Trigger namespace deletion (ignore if already gone).
			_ = client.IgnoreNotFound(k8sClient.Delete(context.TODO(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
			}))

			// Wait for the namespace to be fully deleted so that the next
			// JustBeforeEach does not try to create resources in a Terminating namespace.
			Eventually(func(g Gomega) {
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: testNamespace}, &corev1.Namespace{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected namespace %s to be gone", testNamespace)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		It("sets observedGeneration after initial reconciliation", func() {
			Eventually(func(g Gomega) {
				current := &v1beta1.ProxySetting{}
				g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ps.GetName(), Namespace: testNamespace}, current)).To(Succeed())

				g.Expect(current.Status.ObservedGeneration).To(
					Equal(current.GetGeneration()),
					"expected status.observedGeneration (%d) to equal metadata.generation (%d)",
					current.Status.ObservedGeneration,
					current.GetGeneration(),
				)

				readyCond := current.Status.Conditions.GetConditionByType(capmeta.ReadyCondition)
				g.Expect(readyCond).NotTo(BeNil(), "expected Ready condition to be set")
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(readyCond.ObservedGeneration).To(Equal(current.GetGeneration()))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		It("updates observedGeneration after a spec change", func() {
			By("waiting for observedGeneration to be set initially", func() {
				Eventually(func(g Gomega) {
					current := &v1beta1.ProxySetting{}
					g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ps.GetName(), Namespace: testNamespace}, current)).To(Succeed())
					g.Expect(current.Status.ObservedGeneration).To(BeNumerically(">=", int64(1)))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})

			By("mutating the spec to increment metadata.generation", func() {
				Eventually(func() error {
					current := &v1beta1.ProxySetting{}
					if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: ps.GetName(), Namespace: testNamespace}, current); err != nil {
						return err
					}

					current.Spec.Subjects = append(current.Spec.Subjects, v1beta1.OwnerSpec{
						Kind: "User",
						Name: "e2e-observed-gen-user-2",
					})

					return k8sClient.Update(context.TODO(), current)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})

			By("verifying observedGeneration matches the updated generation", func() {
				Eventually(func(g Gomega) {
					current := &v1beta1.ProxySetting{}
					g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ps.GetName(), Namespace: testNamespace}, current)).To(Succeed())

					g.Expect(current.Status.ObservedGeneration).To(
						Equal(current.GetGeneration()),
						"expected status.observedGeneration (%d) to match updated metadata.generation (%d)",
						current.Status.ObservedGeneration,
						current.GetGeneration(),
					)

					readyCond := current.Status.Conditions.GetConditionByType(capmeta.ReadyCondition)
					g.Expect(readyCond).NotTo(BeNil(), "expected Ready condition to be set")
					g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(readyCond.ObservedGeneration).To(Equal(current.GetGeneration()))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})
		})
	})
})
