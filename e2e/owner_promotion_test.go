//go:build e2e

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ServiceAccount owner promotion", Ordered, ContinueOnFailure, Serial, Label("owner-promotion"), func() {
	var f *accessFixture
	var promoted, direct, wrong *kubernetes.Clientset
	var actor kubernetes.Interface
	var username string
	const accountName = "promoted-owner"
	ctx := context.Background()

	tokenClient := func(namespace, name string, proxy bool) *kubernetes.Clientset {
		admin, err := kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		token, err := admin.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, name, &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{}}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		base := cfg
		if proxy {
			base = f.proxyConfig
		}
		// No administrator client certificate or impersonation headers may accompany
		// the token: authenticate as the actual ServiceAccount, with native groups.
		config := &rest.Config{Host: base.Host, BearerToken: token.Status.Token, QPS: 200, Burst: 400, Timeout: 10 * time.Second, TLSClientConfig: rest.TLSClientConfig{CAFile: base.CAFile, CAData: base.CAData, ServerName: base.ServerName}}
		result, err := kubernetes.NewForConfig(config)
		Expect(err).NotTo(HaveOccurred())
		return result
	}
	list := func(c kubernetes.Interface, options metav1.ListOptions) ([]string, error) {
		result, err := c.CoreV1().Namespaces().List(ctx, options)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(result.Items))
		for _, ns := range result.Items {
			names = append(names, ns.Name)
		}
		return names, nil
	}

	BeforeAll(func() {
		f = newAccessFixture()
		configuration := &capsulev1beta2.CapsuleConfiguration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "default"}, configuration)).To(Succeed())
		original := configuration.Spec.DeepCopy()
		DeferCleanup(func() {
			Eventually(func() error {
				current := &capsulev1beta2.CapsuleConfiguration{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: configuration.Name}, current); err != nil {
					return err
				}
				current.Spec.AllowServiceAccountPromotion = original.AllowServiceAccountPromotion
				current.Spec.RBAC = original.RBAC
				return k8sClient.Update(ctx, current)
			}, accessTimeout, defaultPollInterval).Should(Succeed())
		})
		Eventually(func() error {
			current := &capsulev1beta2.CapsuleConfiguration{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(configuration), current); err != nil {
				return err
			}
			current.Spec.AllowServiceAccountPromotion = true
			if current.Spec.RBAC == nil {
				current.Spec.RBAC = &capsulev1beta2.RBACConfiguration{}
			}
			current.Spec.RBAC.PromotionClusterRoles = []string{"capsule-namespace-provisioner", "capsule-namespace-deleter", "admin"}
			return k8sClient.Update(ctx, current)
		}, accessTimeout, defaultPollInterval).Should(Succeed())
		f.addTenant("a", capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, f.subjects["owner"].username)}, "a1", "a2")
		f.addTenant("b", capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, f.subjects["other"].username)}, "b1")
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(f.tenants["a"]), current); err != nil {
				return err
			}
			current.Spec.Permissions.AllowOwnerPromotion = true
			return k8sClient.Update(ctx, current)
		}, accessTimeout, defaultPollInterval).Should(Succeed())
		actor = ownerClient(f.tenants["a"].Spec.Owners[0])
		Eventually(func() error {
			_, err := actor.CoreV1().ServiceAccounts(f.namespaces["a1"]).Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: accountName, Labels: map[string]string{capsulemeta.OwnerPromotionLabel: "true"}}}, metav1.CreateOptions{})
			return err
		}, accessTimeout, defaultPollInterval).Should(Succeed())
		f.create(&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: accountName, Namespace: f.namespaces["b1"]}})
		username = "system:serviceaccount:" + f.namespaces["a1"] + ":" + accountName
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(f.tenants["a"]), current)).To(Succeed())
			g.Expect(current.Status.Owners.IsOwner(username, nil)).To(BeTrue())
		}, accessTimeout, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			review := &authorizationv1.SubjectAccessReview{Spec: authorizationv1.SubjectAccessReviewSpec{User: username, Groups: []string{"system:authenticated", "system:serviceaccounts", "system:serviceaccounts:" + f.namespaces["a1"]}, ResourceAttributes: &authorizationv1.ResourceAttributes{Namespace: f.namespaces["a1"], Verb: "create", Resource: "configmaps"}}}
			g.Expect(k8sClient.Create(ctx, review)).To(Succeed())
			g.Expect(review.Status.Allowed).To(BeTrue())
		}, accessTimeout, defaultPollInterval).Should(Succeed())
		promoted, direct, wrong = tokenClient(f.namespaces["a1"], accountName, true), tokenClient(f.namespaces["a1"], accountName, false), tokenClient(f.namespaces["b1"], accountName, true)
		current := &capsulev1beta2.CapsuleConfiguration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "default"}, current)).To(Succeed())
		Expect(current.Status.Users.GetByKinds([]capsulerbac.OwnerKind{capsulerbac.ServiceAccountOwner})).NotTo(ContainElement(username), "this must exercise status ownership without globally configuring the account")
	})

	It("lists the tenant with a real token, enforces isolation, and applies promotion changes", func() {
		Eventually(func() ([]string, error) { return list(promoted, metav1.ListOptions{}) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.namespaces["a1"], f.namespaces["a2"]))
		_, err := list(direct, metav1.ListOptions{})
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "native API must not grant cluster-wide namespace listing: %v", err)
		_, err = list(wrong, metav1.ListOptions{})
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "same account name in another namespace must not inherit ownership: %v", err)
		_, err = promoted.CoreV1().ConfigMaps(f.namespaces["a1"]).Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "owner-access"}}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = promoted.CoreV1().ConfigMaps(f.namespaces["b1"]).Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "forbidden-cross-tenant"}}, metav1.CreateOptions{})
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "cross-tenant write must remain forbidden: %v", err)
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: f.namespaces["b1"], Name: "forbidden-cross-tenant"}, &corev1.ConfigMap{}))).To(BeTrue())
		Expect(promoted.CoreV1().Namespaces().Delete(ctx, f.namespaces["a2"], metav1.DeleteOptions{})).To(Succeed())
		Eventually(func() ([]string, error) { return list(promoted, metav1.ListOptions{}) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.namespaces["a1"]))
		names, err := list(promoted, metav1.ListOptions{FieldSelector: "metadata.name=" + f.namespaces["a2"]})
		Expect(err).NotTo(HaveOccurred())
		Expect(names).To(BeEmpty())
		for _, enabled := range []bool{false, true} {
			Eventually(func() error {
				sa, err := actor.CoreV1().ServiceAccounts(f.namespaces["a1"]).Get(ctx, accountName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if enabled {
					if sa.Labels == nil {
						sa.Labels = map[string]string{}
					}
					sa.Labels[capsulemeta.OwnerPromotionLabel] = "true"
				} else {
					delete(sa.Labels, capsulemeta.OwnerPromotionLabel)
				}
				_, err = actor.CoreV1().ServiceAccounts(sa.Namespace).Update(ctx, sa, metav1.UpdateOptions{})
				return err
			}, accessTimeout, defaultPollInterval).Should(Succeed())
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.Tenant{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(f.tenants["a"]), current)).To(Succeed())
				g.Expect(current.Status.Owners.IsOwner(username, nil)).To(Equal(enabled))
				names, err := list(promoted, metav1.ListOptions{})
				if enabled {
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(names).To(ConsistOf(f.namespaces["a1"]))
				} else {
					g.Expect(apierrors.IsForbidden(err)).To(BeTrue(), "revoked owner must use native authorization: %v", err)
				}
			}, accessTimeout, defaultPollInterval).Should(Succeed())
		}
	})

	It("returns a successful empty list for a recognized owner with no namespaces", func() {
		emptyName := "system:serviceaccount:" + f.namespaces["identities"] + ":reader"
		f.addTenant("empty", capsulerbac.OwnerListSpec{f.owner(capsulerbac.ServiceAccountOwner, emptyName)})
		empty := tokenClient(f.namespaces["identities"], "reader", true)
		// An empty access set must remain empty even with the former marker present.
		Eventually(func() error {
			ns := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: f.namespaces["b1"]}, ns); err != nil {
				return err
			}
			ns.Labels["dontexistsignoreme"] = "true"
			return k8sClient.Update(ctx, ns)
		}, accessTimeout, defaultPollInterval).Should(Succeed())
		Eventually(func() ([]string, error) { return list(empty, metav1.ListOptions{}) }, accessTimeout, defaultPollInterval).Should(BeEmpty())
		_, err := empty.CoreV1().ConfigMaps(f.namespaces["b1"]).List(ctx, metav1.ListOptions{})
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "empty ownership cannot grant foreign namespace access: %v", err)
	})

	It("records direct and proxied namespace-list latency for the same authorized selection", func() {
		role := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: f.id + "-measured-list"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"namespaces"}, ResourceNames: []string{f.namespaces["a1"]}, Verbs: []string{"list"}}}}
		f.create(role)
		f.create(&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: role.Name}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: role.Name}, Subjects: []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Namespace: f.namespaces["a1"], Name: accountName}}})
		options := metav1.ListOptions{FieldSelector: "metadata.name=" + f.namespaces["a1"]}
		Eventually(func() ([]string, error) { return list(direct, options) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.namespaces["a1"]))
		Eventually(func() ([]string, error) { return list(promoted, options) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.namespaces["a1"]))
		samples := map[string][]time.Duration{"direct": {}, "proxy": {}}
		for i := range 30 {
			order := []string{"direct", "proxy"}
			if i%2 != 0 {
				order = []string{"proxy", "direct"}
			}
			for _, name := range order {
				c := direct
				if name == "proxy" {
					c = promoted
				}
				start := time.Now()
				names, err := list(c, options)
				duration := time.Since(start)
				Expect(err).NotTo(HaveOccurred())
				Expect(names).To(ConsistOf(f.namespaces["a1"]))
				samples[name] = append(samples[name], duration)
			}
		}
		for name, durations := range samples {
			sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
			var total time.Duration
			for _, elapsed := range durations {
				total += elapsed
			}
			AddReportEntry("namespace-list-"+name, map[string]any{"samples": len(durations), "median": durations[len(durations)/2].String(), "p95": durations[len(durations)*95/100].String(), "requests_per_second": float64(len(durations)) / total.Seconds()})
		}
	})
})
