package e2e_test

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
									APIGroups:  []string{""},
									Resources:  []string{"namespaces"},
									Operations: []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationList},
									Selector: &metav1.LabelSelector{
										MatchLabels: e2eLabels(),
									},
								},
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

		for _, setting := range settings {
			s := setting
			Eventually(func() error {
				s.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), s)
			}).Should(Succeed())
		}

		// Verify observedGeneration is set after reconciliation for each created resource
		for _, setting := range settings {
			name := setting.GetName()

			Eventually(func(g Gomega) {
				current := &v1beta1.GlobalProxySettings{}
				g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: name}, current)).To(Succeed())

				g.Expect(current.Status.ObservedGeneration).To(
					Equal(current.GetGeneration()),
					"expected GlobalProxySettings %q status.observedGeneration (%d) to equal metadata.generation (%d)",
					name,
					current.Status.ObservedGeneration,
					current.GetGeneration(),
				)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		// Load Alice's kubeconfig
		aliceClient, err = loadKubeConfig("alice")
		Expect(err).NotTo(HaveOccurred())

		// Load Bob's kubeconfig
		bobClient, err = loadKubeConfig("bob")
		Expect(err).NotTo(HaveOccurred())
	})

	JustAfterEach(func() {
		// Namespace does not support DELETE collection, so it must be removed by name.
		Eventually(func() error {
			return client.IgnoreNotFound(k8sClient.Delete(context.Background(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "globally-visible-namespace"},
			}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		// Define Resources which are lifecycled after each test
		resourcesToClean := []client.Object{
			&capsulev1beta2.Tenant{},
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

	It("Allows listing and getting namespaces and tenants selected by global settings", func() {
		selectedTenant := &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "globally-visible-tenant",
				Labels: e2eLabels(),
			},
			Spec: capsulev1beta2.TenantSpec{
				Owners: capsulerbac.OwnerListSpec{{
					CoreOwnerSpec: capsulerbac.CoreOwnerSpec{
						UserSpec: capsulerbac.UserSpec{Name: "global-resource-owner", Kind: "User"},
					},
				}},
			},
		}
		Eventually(func() error {
			selectedTenant.ResourceVersion = ""

			return k8sClient.Create(context.Background(), selectedTenant)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		selectedNamespace := NewNamespace("globally-visible-namespace", e2eLabels())
		selectedNamespace.Labels["capsule.clastix.io/tenant"] = selectedTenant.Name
		NamespaceCreation(selectedNamespace, selectedTenant.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		listTenants := func() ([]string, error) {
			raw, err := aliceClient.RESTClient().Get().
				AbsPath("/apis/capsule.clastix.io/v1beta2/tenants").
				DoRaw(context.Background())
			if err != nil {
				return nil, err
			}

			list := &capsulev1beta2.TenantList{}
			if err := json.Unmarshal(raw, list); err != nil {
				return nil, err
			}

			names := make([]string, 0, len(list.Items))
			for i := range list.Items {
				names = append(names, list.Items[i].Name)
			}

			return names, nil
		}

		Eventually(listTenants, defaultTimeoutInterval, defaultPollInterval).
			Should(ConsistOf(selectedTenant.Name), "Alice should list a globally selected tenant she does not own")
		Eventually(func() error {
			_, err := aliceClient.RESTClient().Get().
				AbsPath("/apis/capsule.clastix.io/v1beta2/tenants/" + selectedTenant.Name).
				DoRaw(context.Background())

			return err
		}, defaultTimeoutInterval, defaultPollInterval).
			Should(Succeed(), "Alice should get a globally selected tenant she does not own")

		Eventually(func() ([]string, error) {
			list, err := aliceClient.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return nil, err
			}

			names := make([]string, 0, len(list.Items))
			for i := range list.Items {
				names = append(names, list.Items[i].Name)
			}

			return names, nil
		}, defaultTimeoutInterval, defaultPollInterval).
			Should(ConsistOf(selectedNamespace.Name), "Alice should list a globally selected namespace she does not own")
		Eventually(func() error {
			_, err := aliceClient.CoreV1().Namespaces().Get(context.Background(), selectedNamespace.Name, metav1.GetOptions{})

			return err
		}, defaultTimeoutInterval, defaultPollInterval).
			Should(Succeed(), "Alice should get a globally selected namespace she does not own")
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
