//go:build e2e

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/controllers"
)

var _ = Describe("Global namespace GET grants", Ordered, ContinueOnFailure, Label("namespace-global-rules"), func() {
	ctx := context.Background()
	namespaces := schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	var f *accessFixture
	var setting *v1beta1.GlobalProxySettings

	listNames := func(subject string) ([]string, error) {
		list, err := f.subjects[subject].client.Resource(namespaces).List(ctx, f.listOptions())
		if err != nil {
			return nil, err
		}

		names := make([]string, 0, len(list.Items))
		for _, ns := range list.Items {
			names = append(names, ns.GetName())
		}

		return names, nil
	}
	expectList := func(subject string, keys ...string) {
		names := make([]string, 0, len(keys))
		for _, key := range keys {
			names = append(names, f.namespaces[key])
		}

		Eventually(func() ([]string, error) { return listNames(subject) }, accessTimeout, defaultPollInterval).
			Should(ConsistOf(names), "%s namespace LIST", subject)
	}
	expectGet := func(subject, key string) {
		Eventually(func(g Gomega) {
			ns, err := f.subjects[subject].client.Resource(namespaces).Get(ctx, f.namespaces[key], metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(ns.GetName()).To(Equal(f.namespaces[key]))
			g.Expect(ns.GetKind()).To(Equal("Namespace"))
			g.Expect(ns.GetAPIVersion()).To(Equal("v1"))
			g.Expect(ns.GetLabels()).To(HaveKeyWithValue(accessRunLabel, f.id))
		}, accessTimeout, defaultPollInterval).Should(Succeed(), "%s GET %s", subject, key)
	}
	expectNotFound := func(subject, key string) {
		Eventually(func() error {
			_, err := f.subjects[subject].client.Resource(namespaces).Get(ctx, f.namespaces[key], metav1.GetOptions{})
			return err
		}, accessTimeout, defaultPollInterval).Should(WithTransform(apierrors.IsNotFound, BeTrue()), "%s GET %s must return Kubernetes NotFound", subject, key)
	}
	grant := func(operations ...v1beta1.ClusterResourceOperation) v1beta1.GlobalProxySettingsSpec {
		return v1beta1.GlobalProxySettingsSpec{Rules: []v1beta1.GlobalSubjectSpec{{
			Subjects: []v1beta1.GlobalSubject{
				{Kind: capsulerbac.UserOwner, Name: f.subjects["owner"].username},
				{Kind: capsulerbac.UserOwner, Name: f.subjects["outsider"].username},
				{Kind: capsulerbac.GroupOwner, Name: f.id + "-team"},
				{Kind: capsulerbac.ServiceAccountOwner, Name: f.subjects["serviceaccount"].username},
			},
			ClusterResources: []v1beta1.ClusterResource{{
				APIGroups: []string{""}, Resources: []string{"namespaces"}, Operations: operations,
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{accessRunLabel: f.id, accessVariantLabel: "shared"}},
			}},
		}}}
	}
	updateSetting := func(spec v1beta1.GlobalProxySettingsSpec) {
		Eventually(func() error {
			current := &v1beta1.GlobalProxySettings{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(setting), current); err != nil {
				return err
			}
			current.Spec = spec
			return k8sClient.Update(ctx, current)
		}, accessTimeout, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &v1beta1.GlobalProxySettings{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(setting), current)).To(Succeed())
			g.Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))
		}, accessTimeout, defaultPollInterval).Should(Succeed())
	}
	setVariant := func(key, variant string) {
		Eventually(func() error {
			ns := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: f.namespaces[key]}, ns); err != nil {
				return err
			}
			before := ns.DeepCopy()
			ns.Labels[accessVariantLabel] = variant
			return k8sClient.Patch(ctx, ns, client.MergeFrom(before))
		}, accessTimeout, defaultPollInterval).Should(Succeed())
	}
	reviewClient := func(base *rest.Config, subject string, extraGroups ...string) *kubernetes.Clientset {
		identity := f.subjects[subject]
		c := rest.CopyConfig(base)
		c.Impersonate = rest.ImpersonationConfig{UserName: identity.username, Groups: append(append([]string{}, identity.groups...), extraGroups...)}
		c.QPS = -1 // Measure the proxy/API server without caller-side throttling.
		cl, err := kubernetes.NewForConfig(c)
		Expect(err).NotTo(HaveOccurred())
		return cl
	}
	review := func(cl *kubernetes.Clientset, key string) (*authorizationv1.SelfSubjectAccessReview, error) {
		return cl.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{
				Version: "v1", Resource: "namespaces", Verb: "get", Name: f.namespaces[key],
			}},
		}, metav1.CreateOptions{})
	}

	BeforeAll(func() {
		f = newAccessFixture()
		f.addTenant("a", capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, f.subjects["owner"].username, "admin")}, "a")
		f.addTenant("b", capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, f.subjects["other"].username, "admin")}, "b-shared", "b-private")
		setVariant("b-shared", "shared")
		setVariant("b-private", "private")
		for _, key := range []string{"shared", "excluded", "partial"} {
			labels := map[string]string{accessRunLabel: f.id, accessVariantLabel: "shared"}
			if key == "excluded" {
				labels[accessVariantLabel] = "private"
			}
			if key == "partial" {
				delete(labels, accessRunLabel)
			}
			ns := NewNamespace(f.id+"-"+key, labels)
			f.create(ns)
			f.namespaces[key] = ns.Name
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())
			Expect(ns.OwnerReferences).To(BeEmpty(), "the regression requires a namespace without a Tenant owner reference")
		}

		// Establish the ordinary tenant path and a cached denial before adding
		// global access for Capsule identities that do not own a Tenant.
		expectGet("owner", "a")
		expectNotFound("outsider", "shared")
		setting = &v1beta1.GlobalProxySettings{ObjectMeta: metav1.ObjectMeta{Name: f.id + "-namespace-grants"}, Spec: grant()}
		f.create(setting)
	})

	BeforeEach(func() {
		updateSetting(grant())
		expectGet("outsider", "shared")
	})

	DescribeTable("honors namespace grants without weakening tenant or subject boundaries", func(operations []v1beta1.ClusterResourceOperation, lists bool) {
		updateSetting(grant(operations...))
		for _, subject := range []string{"owner", "outsider", "group", "serviceaccount"} {
			expectGet(subject, "shared")
			expectGet(subject, "b-shared")
			var visible []string
			if subject == "owner" {
				visible = append(visible, "a")
				expectGet(subject, "a")
			} else {
				expectNotFound(subject, "a")
			}
			if lists {
				visible = append(visible, "shared", "b-shared")
			}
			expectList(subject, visible...)
			for _, key := range []string{"excluded", "partial", "b-private"} {
				expectNotFound(subject, key)
			}
		}

		for _, subject := range []string{"coowner", "group-name-user", "wrong-serviceaccount"} {
			expectList(subject)
			expectNotFound(subject, "shared")
			expectNotFound(subject, "b-shared")
		}
		expectList("other", "b-shared", "b-private")
		expectGet("other", "b-private")
		expectNotFound("other", "shared")
		expectNotFound("other", "a")
	},
		Entry("omitted operations permit LIST and GET", nil, true),
		Entry("legacy LIST also permits GET", []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationList}, true),
		Entry("GET-only grants do not add namespaces to LIST", []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationGet}, false),
	)

	DescribeTable("does not apply namespace labels to unrelated resource rules", func(group, resource string) {
		spec := grant()
		spec.Rules[0].ClusterResources[0].APIGroups = []string{group}
		spec.Rules[0].ClusterResources[0].Resources = []string{resource}
		updateSetting(spec)
		expectGet("owner", "a")
		expectGet("other", "b-private")
		for _, subject := range []string{"owner", "outsider"} {
			expectNotFound(subject, "shared")
			expectNotFound(subject, "b-shared")
		}
		expectList("owner", "a")
		expectList("outsider")
	},
		Entry("different API group", "apps", "namespaces"),
		Entry("different resource", "", "nodes"),
	)

	It("advertises named GET access consistently with namespace visibility", func() {
		for _, subject := range []string{"outsider", "owner", "other", "group", "serviceaccount"} {
			cl := reviewClient(f.proxyConfig, subject)
			for _, key := range []string{"shared", "b-shared", "excluded", "b-private", "a"} {
				allowed := key == "shared" || key == "b-shared" || (subject == "owner" && key == "a")
				if subject == "other" {
					allowed = key == "b-shared" || key == "b-private"
				}
				Eventually(func(g Gomega) {
					result, err := review(cl, key)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(result.Status.Allowed).To(Equal(allowed))
				}, accessTimeout, defaultPollInterval).Should(Succeed(), "%s named GET review for %s", subject, key)
				if allowed {
					expectGet(subject, key)
				} else {
					expectNotFound(subject, key)
				}
			}
		}
	})

	It("updates reflected namespace self-reviews when a RoleBinding is removed", func() {
		cl := reviewClient(f.proxyConfig, "coowner")
		expectNotFound("coowner", "a")
		role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "namespace-review", Namespace: f.namespaces["a"]}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"list"}}}}
		binding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: role.Name, Namespace: role.Namespace, Labels: map[string]string{controllers.RoleBindingReflectionLabel: "true"}}, Subjects: []rbacv1.Subject{{Kind: "User", APIGroup: rbacv1.GroupName, Name: f.subjects["coowner"].username}}, RoleRef: rbacv1.RoleRef{Kind: "Role", APIGroup: rbacv1.GroupName, Name: role.Name}}
		f.create(role)
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, role))).To(Succeed())
		})
		f.create(binding)
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, binding))).To(Succeed())
			expectNotFound("coowner", "a")
		})
		expectGet("coowner", "a")
		Eventually(func(g Gomega) {
			result, err := review(cl, "a")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Status.Allowed).To(BeTrue())
		}, accessTimeout, defaultPollInterval).Should(Succeed())
		expectNotFound("coowner", "b-private")
		Expect(k8sClient.Delete(ctx, binding)).To(Succeed())
		expectNotFound("coowner", "a")
		Eventually(func(g Gomega) {
			result, err := review(cl, "a")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Status.Allowed).To(BeFalse())
		}, accessTimeout, defaultPollInterval).Should(Succeed())
	})

	It("records direct and proxied named-review latency with equivalent native read permissions", func() {
		role := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: f.id + "-native-get"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"namespaces"}, Verbs: []string{"get"}}}}
		f.create(role)
		f.create(&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: role.Name}, RoleRef: rbacv1.RoleRef{Kind: "ClusterRole", APIGroup: rbacv1.GroupName, Name: role.Name}, Subjects: []rbacv1.Subject{{Kind: "User", APIGroup: rbacv1.GroupName, Name: f.subjects["outsider"].username}}})
		for _, groupCount := range []int{0, 8} {
			groups := make([]string, groupCount)
			for i := range groups {
				groups[i] = fmt.Sprintf("%s-extra-%d", f.id, i)
			}
			for _, endpoint := range []*rest.Config{cfg, f.proxyConfig} {
				cl := reviewClient(endpoint, "outsider", groups...)
				for _, key := range []string{"shared", "excluded"} {
					allowed := endpoint == cfg || key == "shared"
					Eventually(func(g Gomega) {
						result, err := review(cl, key)
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(result.Status.Allowed).To(Equal(allowed))
					}, accessTimeout, defaultPollInterval).Should(Succeed())
					const requests = 10
					start := time.Now()
					for range requests {
						result, err := review(cl, key)
						Expect(err).NotTo(HaveOccurred())
						Expect(result.Status.Allowed).To(Equal(allowed))
					}
					elapsed := time.Since(start)
					fmt.Fprintf(GinkgoWriter, "namespace GET review proxy=%t extra-groups=%d selected=%t requests=%d mean=%s throughput=%.1f/s\n", endpoint == f.proxyConfig, groupCount, key == "shared", requests, elapsed/requests, float64(requests)/elapsed.Seconds())
				}
			}
		}
	})

	It("enforces named namespace reviews with resource caching disabled", func() {
		endpoint := globalAccessProxy(f, false, "--require-capsule-user=true", "--disable-caching=true")
		cl := reviewClient(endpoint, "outsider")
		Eventually(func(g Gomega) {
			ns, err := cl.CoreV1().Namespaces().Get(ctx, f.namespaces["shared"], metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(ns.Name).To(Equal(f.namespaces["shared"]))
		}, accessTimeout, defaultPollInterval).Should(Succeed())
		for _, key := range []string{"shared", "excluded"} {
			allowed := key == "shared"
			Eventually(func(g Gomega) {
				result, err := review(cl, key)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(result.Status.Allowed).To(Equal(allowed))
			}, accessTimeout, defaultPollInterval).Should(Succeed())
			const requests = 10
			start := time.Now()
			for range requests {
				result, err := review(cl, key)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Status.Allowed).To(Equal(allowed))
			}
			elapsed := time.Since(start)
			fmt.Fprintf(GinkgoWriter, "namespace GET review resource-cache=false selected=%t requests=%d mean=%s throughput=%.1f/s (includes pod port-forward transport)\n", allowed, requests, elapsed/requests, float64(requests)/elapsed.Seconds())
		}
		_, err := cl.CoreV1().Namespaces().Get(ctx, f.namespaces["excluded"], metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "uncached GET must enforce the same selector: %v", err)
	})

	It("keeps mutations subject to native Kubernetes RBAC", func() {
		for _, subject := range []string{"owner", "outsider"} {
			expectGet(subject, "shared")
			cl := f.subjects[subject].client.Resource(namespaces)
			err := cl.Delete(ctx, f.namespaces["shared"], metav1.DeleteOptions{})
			Expect(apierrors.IsForbidden(err)).To(BeTrue(), "a global read grant must not allow DELETE: %v", err)
			_, err = cl.Patch(ctx, f.namespaces["shared"], types.MergePatchType, []byte(`{"metadata":{"labels":{"unauthorized":"true"}}}`), metav1.PatchOptions{})
			Expect(apierrors.IsForbidden(err)).To(BeTrue(), "a global read grant must not allow PATCH: %v", err)
		}
		ns := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: f.namespaces["shared"]}, ns)).To(Succeed())
		Expect(ns.DeletionTimestamp).To(BeNil())
		Expect(ns.Labels).NotTo(HaveKey("unauthorized"))
	})

	It("rechecks namespace labels and updated global selectors", func() {
		DeferCleanup(setVariant, "shared", "shared")
		setVariant("shared", "private")
		expectGet("outsider", "b-shared")
		expectNotFound("outsider", "shared")
		expectNotFound("owner", "shared")
		expectList("owner", "a", "b-shared")
		expectList("outsider", "b-shared")

		spec := grant()
		spec.Rules[0].ClusterResources[0].Selector.MatchLabels[accessVariantLabel] = "private"
		updateSetting(spec)
		expectGet("outsider", "excluded")
		expectGet("outsider", "b-private")
		expectList("outsider", "shared", "excluded", "b-private")
		expectNotFound("outsider", "b-shared")
		expectList("owner", "a", "shared", "excluded", "b-private")
		expectList("other", "b-shared", "b-private")
		expectNotFound("other", "excluded")
	})

	It("moves grants between subjects without leaking the previous subject's access", func() {
		spec := grant()
		spec.Rules[0].Subjects = []v1beta1.GlobalSubject{{Kind: capsulerbac.UserOwner, Name: f.subjects["coowner"].username}}
		updateSetting(spec)
		expectGet("coowner", "shared")
		expectGet("coowner", "b-shared")
		expectList("coowner", "shared", "b-shared")
		expectNotFound("coowner", "b-private")
		for _, subject := range []string{"owner", "outsider", "group", "serviceaccount"} {
			expectNotFound(subject, "shared")
			expectNotFound(subject, "b-shared")
		}
		expectList("owner", "a")
		expectGet("owner", "a")
		expectList("outsider")
		expectList("other", "b-shared", "b-private")
	})

	It("revokes deleted global settings while retaining tenant ownership", func() {
		Expect(k8sClient.Delete(ctx, setting)).To(Succeed())
		for _, subject := range []string{"owner", "outsider", "group", "serviceaccount"} {
			expectNotFound(subject, "shared")
			expectNotFound(subject, "b-shared")
		}
		expectGet("owner", "a")
		expectList("owner", "a")
		expectList("outsider")
		expectGet("other", "b-private")
		expectList("other", "b-shared", "b-private")
	})
})
