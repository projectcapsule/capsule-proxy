//go:build e2e

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Proxy health probes", Ordered, ContinueOnFailure, Label("probes"), func() {
	var f *accessFixture
	var pods []corev1.Pod
	var admin *kubernetes.Clientset
	resource := accessResources()[1] // ConfigMaps do not require workload scheduling.

	BeforeAll(func() {
		f = newAccessFixture()
		configuration := &capsulev1beta2.CapsuleConfiguration{}
		Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: "default"}, configuration)).To(Succeed())
		groups := configuration.Status.Users.GetByKinds([]capsulerbac.OwnerKind{capsulerbac.GroupOwner})
		Expect(groups).NotTo(BeEmpty(), "the test environment must have a Capsule user group")
		for _, subject := range []string{"owner", "other", "outsider"} {
			f.addSubject(subject, f.subjects[subject].username, groups[0])
		}
		f.addTenant("a", capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, f.subjects["owner"].username)})
		f.addTenant("b", capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, f.subjects["other"].username)})
		for _, key := range []string{"a", "b"} {
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Name:   f.tenants[key].Name + "-ns",
				Labels: map[string]string{capsulemeta.TenantLabel: f.tenants[key].Name, accessRunLabel: f.id},
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(
					f.tenants[key], capsulev1beta2.GroupVersion.WithKind("Tenant"),
				)},
			}}
			subject := "owner"
			if key == "b" {
				subject = "other"
			}
			ownerConfig := rest.CopyConfig(cfg)
			ownerConfig.Impersonate = rest.ImpersonationConfig{UserName: f.subjects[subject].username, Groups: f.subjects[subject].groups}
			owner, err := kubernetes.NewForConfig(ownerConfig)
			Expect(err).NotTo(HaveOccurred())
			_, err = owner.CoreV1().Namespaces().Create(context.Background(), namespace, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				Eventually(func() error {
					return client.IgnoreNotFound(k8sClient.Delete(context.Background(), namespace))
				}, accessTimeout, defaultPollInterval).Should(Succeed())
				Eventually(func() bool {
					return apierrors.IsNotFound(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(namespace), &corev1.Namespace{}))
				}, accessTimeout, defaultPollInterval).Should(BeTrue())
			})
			f.namespaces[key+"1"] = namespace.Name
		}
		f.seed(resource, "a", "a1")
		f.seed(resource, "b", "b1")
		f.waitPermission("owner", "a1", "", "configmaps", true)
		f.waitPermission("other", "b1", "", "configmaps", true)
		f.expectList("owner", resource, "a1")
		f.expectList("other", resource, "b1")

		selector := os.Getenv("E2E_PROXY_POD_SELECTOR")
		if selector == "" {
			selector = "app.kubernetes.io/name=capsule-proxy"
		}
		list := &corev1.PodList{}
		Expect(k8sClient.List(context.Background(), list, &client.ListOptions{Raw: &metav1.ListOptions{LabelSelector: selector}})).To(Succeed())
		for _, pod := range list.Items {
			if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
				continue
			}
			for _, container := range pod.Spec.Containers {
				if container.Name == "capsule-proxy" {
					pods = append(pods, pod)
					break
				}
			}
		}
		Expect(pods).NotTo(BeEmpty(), "the selector must match the changed proxy pods")
		config := rest.CopyConfig(cfg)
		config.Timeout = f.proxyConfig.Timeout
		var err error
		admin, err = kubernetes.NewForConfig(config)
		Expect(err).NotTo(HaveOccurred())
		for _, pod := range pods {
			body, err := admin.CoreV1().Pods(pod.Namespace).ProxyGet("http", pod.Name, "8081", "/readyz/", nil).DoRaw(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(string(body)).To(Equal("ok"))
		}
	})

	for _, path := range []string{"readyz", "healthz"} {
		It("bounds retained goroutines during repeated "+path+" checks and preserves tenant access", func() {
			for _, pod := range pods {
				goroutines := func() float64 {
					body, err := admin.CoreV1().Pods(pod.Namespace).ProxyGet("http", pod.Name, "8080", "/metrics", nil).DoRaw(context.Background())
					Expect(err).NotTo(HaveOccurred())
					for _, line := range strings.Split(string(body), "\n") {
						if value, ok := strings.CutPrefix(line, "go_goroutines "); ok {
							count, err := strconv.ParseFloat(value, 64)
							Expect(err).NotTo(HaveOccurred())
							return count
						}
					}
					Fail("proxy metrics must include go_goroutines")
					return 0
				}
				before := goroutines()
				for range 200 {
					body, err := admin.CoreV1().Pods(pod.Namespace).ProxyGet("http", pod.Name, "8081", "/"+path+"/", nil).DoRaw(context.Background())
					Expect(err).NotTo(HaveOccurred())
					Expect(string(body)).To(Equal("ok"))
				}
				after := goroutines()
				fmt.Fprintf(GinkgoWriter, "%s/%s %s: goroutines before=%.0f after=%.0f\n", pod.Namespace, pod.Name, path, before, after)
				// A leaking HTTPS readiness transport retains three goroutines per
				// probe. Allow unrelated controller/watch activity, well below 600.
				Expect(after - before).To(BeNumerically("<", 50))
			}

			f.expectList("owner", resource, "a1")
			f.expectList("other", resource, "b1")
			for _, subject := range []string{"owner", "outsider"} {
				_, err := f.list(subject, resource, f.namespaces["b1"], f.listOptions())
				Expect(apierrors.IsForbidden(err)).To(BeTrue(), "health checks must not grant %s cross-tenant access: %v", subject, err)
			}
			f.expectList("outsider", resource)
		})
	}
})
