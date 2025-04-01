package e2e_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
)

var _ = Describe("GlobalProxySettings", func() {
	var aliceClient, bobClient *kubernetes.Clientset

	BeforeEach(func() {
		var err error

		aliceClient, err = loadKubeConfig("alice")
		Expect(err).ToNot(HaveOccurred())
		bobClient, err = loadKubeConfig("bob")
		Expect(err).ToNot(HaveOccurred())

		// Create Global Proxy Settings
		settings := []*v1beta1.GlobalProxySettings{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "global-proxy-settings",
					Labels: e2eLabels(),
				},
				Spec: v1beta1.GlobalProxySettingsSpec{
					Rules: []v1beta1.GlobalSubjectSpec{
						{
							ClusterResources: []v1beta1.ClusterResource{
								{
									APIGroups:  []string{"rbac.authorization.k8s.io/*"},
									Resources:  []string{"*"},
									Operations: []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationList},
									Selector: &metav1.LabelSelector{
										MatchLabels: e2eLabels(),
									},
								},
								{
									APIGroups:  []string{"capsule.clastix.io/*"},
									Resources:  []string{"*"},
									Operations: []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationList},
									Selector: &metav1.LabelSelector{
										MatchLabels: e2eLabels(),
									},
								},
							},
							Subjects: []v1beta1.GlobalSubject{
								{
									Kind: "User",
									Name: "alice",
								},
								{
									Kind: "User",
									Name: "bob",
								},
							},
						},
					},
				},
			},
		}

		for _, tran := range settings {
			Eventually(func() error {
				tran.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), tran)
			}).Should(Succeed())
		}

		// Load Alice's kubeconfig
		aliceClient, err = loadKubeConfig("alice")
		Expect(err).NotTo(HaveOccurred())

		// Load Bob's kubeconfig
		bobClient, err = loadKubeConfig("bob")
		Expect(err).NotTo(HaveOccurred())
	})

	JustAfterEach(func() {
		// Define Resources which are lifecycled after each test
		resourcesToClean := []client.Object{
			&v1beta1.GlobalProxySettings{},
			&rbacv1.ClusterRole{},
		}

		Eventually(func() error {
			return cleanResources(resourcesToClean, e2eSelector())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("Allow listing specific clusterroles (without tenants)", func() {
		roles := []*rbacv1.ClusterRole{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "tenant-viewer",
					Labels: e2eLabels(),
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"pods"},
						Verbs:     []string{"list"},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "tenant-editor",
					Labels: e2eLabels(),
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"pods"},
						Verbs:     []string{"*"},
					},
				},
			},
		}

		for _, role := range roles {
			Eventually(func() error {
				role.ResourceVersion = ""

				return k8sClient.Create(context.Background(), role)
			}).Should(Succeed())
		}

		listClusterRoles := func(clientset *kubernetes.Clientset) ([]string, error) {
			clusterRoles, err := clientset.RbacV1().ClusterRoles().List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			var roleNames []string
			for _, role := range clusterRoles.Items {
				roleNames = append(roleNames, role.Name)
			}

			return roleNames, nil
		}

		// Should only list the clusterroles that are allowed by the GlobalProxySettings
		expectedRoles := []string{"tenant-editor", "tenant-viewer"}

		// Check Alice's access to ClusterRoles
		Eventually(func() ([]string, error) {
			return listClusterRoles(aliceClient)
		}).Should(Equal(expectedRoles), "Alice should only have access to the specified cluster roles")

		// Check Bob's access to ClusterRoles (must contain only the expected roles)
		Eventually(func() ([]string, error) {
			return listClusterRoles(bobClient)
		}).Should(Equal(expectedRoles), "Bob should only have access to the specified cluster roles")
	})

	It("Should only allow listing clusterroles, but deny create, update, delete", func() {
		roleToCreate := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "unauthorized-clusterrole",
				Labels: e2eLabels(),
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"get"},
				},
			},
		}

		attemptCreate := func(clientset *kubernetes.Clientset) error {
			_, err := clientset.RbacV1().ClusterRoles().Create(context.Background(), roleToCreate, metav1.CreateOptions{})
			return err
		}

		attemptUpdate := func(clientset *kubernetes.Clientset) error {
			role, err := clientset.RbacV1().ClusterRoles().Get(context.Background(), "tenant-viewer", metav1.GetOptions{})
			if err != nil {
				return err
			}
			role.Annotations = map[string]string{"updated": "true"}
			_, err = clientset.RbacV1().ClusterRoles().Update(context.Background(), role, metav1.UpdateOptions{})
			return err
		}

		attemptDelete := func(clientset *kubernetes.Clientset) error {
			return clientset.RbacV1().ClusterRoles().Delete(context.Background(), "tenant-viewer", metav1.DeleteOptions{})
		}

		By("Denying create/update/delete for Alice")
		Expect(attemptCreate(aliceClient)).To(HaveOccurred(), "Alice should not be able to create ClusterRoles")
		Expect(attemptUpdate(aliceClient)).To(HaveOccurred(), "Alice should not be able to update ClusterRoles")
		Expect(attemptDelete(aliceClient)).To(HaveOccurred(), "Alice should not be able to delete ClusterRoles")

		By("Denying create/update/delete for Bob")
		Expect(attemptCreate(bobClient)).To(HaveOccurred(), "Bob should not be able to create ClusterRoles")
		Expect(attemptUpdate(bobClient)).To(HaveOccurred(), "Bob should not be able to update ClusterRoles")
		Expect(attemptDelete(bobClient)).To(HaveOccurred(), "Bob should not be able to delete ClusterRoles")
	})
})
