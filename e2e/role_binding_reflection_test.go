package e2e_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/controllers"
)

var _ = Describe("RoleBinding reflection", func() {
	const (
		reflectedTenantName = "rbac-reflected-tenant"
		privateTenantName   = "rbac-private-tenant"
		reflectedNamespace  = "rbac-reflected-namespace"
		privateNamespace    = "rbac-private-namespace"
	)

	owner := capsulerbac.OwnerListSpec{{
		CoreOwnerSpec: capsulerbac.CoreOwnerSpec{
			UserSpec: capsulerbac.UserSpec{Name: "alice", Kind: "User"},
		},
	}}

	reflectedTenant := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: reflectedTenantName, Labels: e2eLabels()},
		Spec: capsulev1beta2.TenantSpec{
			Owners: owner,
			AdditionalRoleBindings: []capsulerbac.AdditionalRoleBindingsSpec{{
				ClusterRoleName: "view",
				Subjects: []rbacv1.Subject{{
					Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: "bob",
				}},
				Labels: map[string]string{controllers.RoleBindingReflectionLabel: "true"},
			}},
		},
	}
	privateTenant := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: privateTenantName, Labels: e2eLabels()},
		Spec:       capsulev1beta2.TenantSpec{Owners: owner},
	}

	BeforeEach(func() {
		for _, tenant := range []*capsulev1beta2.Tenant{reflectedTenant, privateTenant} {
			tenant := tenant
			Eventually(func() error {
				tenant.ResourceVersion = ""

				return k8sClient.Create(context.Background(), tenant)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		for _, namespace := range []*corev1.Namespace{
			NewNamespace(reflectedNamespace, map[string]string{capsulemeta.TenantLabel: reflectedTenantName}),
			NewNamespace(privateNamespace, map[string]string{capsulemeta.TenantLabel: privateTenantName}),
		} {
			NamespaceCreation(namespace, owner[0], defaultTimeoutInterval).Should(Succeed())
		}

		Eventually(func(g Gomega) {
			bindings := &rbacv1.RoleBindingList{}
			g.Expect(k8sClient.List(context.Background(), bindings,
				client.InNamespace(reflectedNamespace),
				client.MatchingLabels{controllers.RoleBindingReflectionLabel: "true"},
			)).To(Succeed())
			g.Expect(bindings.Items).To(HaveLen(1))
			g.Expect(bindings.Items[0].RoleRef.Kind).To(Equal("ClusterRole"))
			g.Expect(bindings.Items[0].RoleRef.Name).To(Equal("view"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	JustAfterEach(func() {
		for _, name := range []string{reflectedNamespace, privateNamespace} {
			name := name
			Eventually(func() error {
				return client.IgnoreNotFound(k8sClient.Delete(context.Background(), &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: name},
				}))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name}, &corev1.Namespace{})

				return apierrors.IsNotFound(err)
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		}

		Eventually(func() error {
			return cleanResources([]client.Object{&capsulev1beta2.Tenant{}}, e2eSelector())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("lists only the namespace with the labelled additional RoleBinding", func() {
		bobClient, err := loadKubeConfig("bob")
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() ([]string, error) {
			list, err := bobClient.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return nil, err
			}

			names := make([]string, 0, len(list.Items))
			for i := range list.Items {
				names = append(names, list.Items[i].Name)
			}

			return names, nil
		}, defaultTimeoutInterval, defaultPollInterval).
			Should(ConsistOf(reflectedNamespace))
	})

	It("lists Pods only from the tenant with the labelled additional RoleBinding", func() {
		pods := []*corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "reflected-pod", Namespace: reflectedNamespace, Labels: map[string]string{capsulemeta.ManagedByCapsuleLabel: reflectedTenantName}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10"}}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "private-pod", Namespace: privateNamespace, Labels: map[string]string{capsulemeta.ManagedByCapsuleLabel: privateTenantName}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10"}}},
			},
		}
		for _, pod := range pods {
			pod := pod
			Eventually(func() error {
				return k8sClient.Create(context.Background(), pod)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		bobClient, err := loadKubeConfig("bob")
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() ([]string, error) {
			list, err := bobClient.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return nil, err
			}

			names := make([]string, 0, len(list.Items))
			for i := range list.Items {
				names = append(names, list.Items[i].Name)
			}

			return names, nil
		}, defaultTimeoutInterval, defaultPollInterval).
			Should(ConsistOf("reflected-pod"))
	})
})
