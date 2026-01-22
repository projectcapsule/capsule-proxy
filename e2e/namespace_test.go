package e2e_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsuleapi "github.com/projectcapsule/capsule/pkg/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var _ = Describe("Namespaces", func() {
	var aliceClient, bobClient *kubernetes.Clientset

	// Create Global Proxy Settings
	wind := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "wind",
			Labels: e2eLabels(),
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsuleapi.OwnerListSpec{
				{
					CoreOwnerSpec: capsuleapi.CoreOwnerSpec{
						UserSpec: capsuleapi.UserSpec{

							Name: "alice",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	// Create Global Proxy Settings
	solar := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "solar",
			Labels: e2eLabels(),
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsuleapi.OwnerListSpec{
				{
					CoreOwnerSpec: capsuleapi.CoreOwnerSpec{
						UserSpec: capsuleapi.UserSpec{

							Name: "alice",
							Kind: "User",
						},
					},
				},
				{
					CoreOwnerSpec: capsuleapi.CoreOwnerSpec{
						UserSpec: capsuleapi.UserSpec{

							Name: "bob",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	BeforeEach(func() {
		var err error

		aliceClient, err = loadKubeConfig("alice")
		Expect(err).ToNot(HaveOccurred())
		bobClient, err = loadKubeConfig("bob")
		Expect(err).ToNot(HaveOccurred())

		for _, tnt := range []*capsulev1beta2.Tenant{solar, wind} {
			Eventually(func() error {
				tnt.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
		}
	})

	JustAfterEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{solar, wind} {
			Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
		}
	})

	It("Should correctly list", func() {
		nsAlice1 := NewNamespace("")
		nsAlice1.Labels = map[string]string{
			"capsule.clastix.io/tenant": "wind",
		}
		NamespaceCreation(nsAlice1, wind.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		nsAlice2 := NewNamespace("")
		nsAlice2.Labels = map[string]string{
			"capsule.clastix.io/tenant": "wind",
		}
		NamespaceCreation(nsAlice2, wind.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		listNamespaces := func(clientset *kubernetes.Clientset) ([]string, error) {
			ns, err := clientset.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			var nsNames []string
			for _, name := range ns.Items {
				nsNames = append(nsNames, name.Name)
			}

			return nsNames, nil
		}

		Eventually(func() ([]string, error) {
			return listNamespaces(aliceClient)
		}).Should(ConsistOf(nsAlice1.GetName(), nsAlice2.GetName()), "Alice should only have access to the expected namespaces, order does not matter")

		nsBob1 := NewNamespace("")
		nsBob1.Labels = map[string]string{
			"capsule.clastix.io/tenant": "solar",
		}
		NamespaceCreation(nsBob1, solar.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		nsBob2 := NewNamespace("")
		nsBob2.Labels = map[string]string{
			"capsule.clastix.io/tenant": "solar",
		}
		NamespaceCreation(nsBob2, solar.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		Eventually(func() ([]string, error) {
			return listNamespaces(bobClient)
		}).Should(ConsistOf(nsBob1.GetName(), nsBob2.GetName()), "Alice should only have access to the expected namespaces, order does not matter")

	})
})
