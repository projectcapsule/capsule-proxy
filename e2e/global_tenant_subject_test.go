//go:build e2e

package e2e_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
)

var _ = Describe("GlobalProxySettings with Tenant subjects", func() {
	const sharedRole = "global-tenant-subject-role"

	var aliceClient, bobClient *kubernetes.Clientset

	ownedBy := func(user string) capsulerbac.OwnerListSpec {
		return capsulerbac.OwnerListSpec{{
			CoreOwnerSpec: capsulerbac.CoreOwnerSpec{
				UserSpec: capsulerbac.UserSpec{Name: user, Kind: capsulerbac.UserOwner},
			},
		}}
	}

	// canSeeRole reports whether the user can list the shared ClusterRole through the proxy.
	// A forbidden listing counts as not visible.
	canSeeRole := func(clientset *kubernetes.Clientset) bool {
		list, err := clientset.RbacV1().ClusterRoles().List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return false
		}

		for _, role := range list.Items {
			if role.Name == sharedRole {
				return true
			}
		}

		return false
	}

	BeforeEach(func() {
		var err error

		aliceClient, err = loadKubeConfig("alice")
		Expect(err).ToNot(HaveOccurred())
		bobClient, err = loadKubeConfig("bob")
		Expect(err).ToNot(HaveOccurred())

		for _, tnt := range []*capsulev1beta2.Tenant{
			{ObjectMeta: metav1.ObjectMeta{Name: "global-tenant-subject-alice", Labels: e2eLabels()}, Spec: capsulev1beta2.TenantSpec{Owners: ownedBy("alice")}},
			{ObjectMeta: metav1.ObjectMeta{Name: "global-tenant-subject-bob", Labels: e2eLabels()}, Spec: capsulev1beta2.TenantSpec{Owners: ownedBy("bob")}},
		} {
			Eventually(func() error {
				tnt.ResourceVersion = ""

				return k8sClient.Create(context.Background(), tnt)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		role := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: sharedRole, Labels: e2eLabels()},
			Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"list"}}},
		}
		Eventually(func() error {
			role.ResourceVersion = ""

			return k8sClient.Create(context.Background(), role)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		settings := &v1beta1.GlobalProxySettings{
			ObjectMeta: metav1.ObjectMeta{Name: "global-tenant-subject", Labels: e2eLabels()},
			Spec: v1beta1.GlobalProxySettingsSpec{Rules: []v1beta1.GlobalSubjectSpec{{
				Subjects: []v1beta1.GlobalSubject{{Kind: v1beta1.GlobalSubjectKindTenant, Name: "global-tenant-subject-alice"}},
				ClusterResources: []v1beta1.ClusterResource{{
					APIGroups:  []string{rbacv1.GroupName},
					Resources:  []string{"clusterroles"},
					Operations: []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationList},
					Selector:   &metav1.LabelSelector{MatchLabels: e2eLabels()},
				}},
			}}},
		}
		Eventually(func() error {
			settings.ResourceVersion = ""

			return k8sClient.Create(context.Background(), settings)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			current := &v1beta1.GlobalProxySettings{}
			g.Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: settings.Name}, current)).To(Succeed())
			g.Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	JustAfterEach(func() {
		resources := []client.Object{
			&capsulev1beta2.Tenant{},
			&v1beta1.GlobalProxySettings{},
			&rbacv1.ClusterRole{},
		}
		Eventually(func() error {
			return cleanResources(resources, e2eSelector())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("grants the rule to the owners of the referenced Tenant only and follows ownership changes", func() {
		By("granting access to the owner of the referenced Tenant")
		Eventually(func() bool { return canSeeRole(aliceClient) }, defaultTimeoutInterval, defaultPollInterval).
			Should(BeTrue(), "the owner of the referenced Tenant should list the ClusterRole")

		By("denying the owner of another Tenant")
		Consistently(func() bool { return canSeeRole(bobClient) }, 5*defaultPollInterval, defaultPollInterval).
			Should(BeFalse(), "the owner of another Tenant should not list the ClusterRole")

		By("revoking access once the user is no longer an owner")
		tnt := &capsulev1beta2.Tenant{}
		Eventually(func() error {
			if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "global-tenant-subject-alice"}, tnt); err != nil {
				return err
			}

			tnt.Spec.Owners = ownedBy("global-tenant-subject-new-owner")

			return k8sClient.Update(context.Background(), tnt)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		Eventually(func() bool { return canSeeRole(aliceClient) }, defaultTimeoutInterval, defaultPollInterval).
			Should(BeFalse(), "a former owner should lose the grant")
	})
})
