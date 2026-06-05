package e2e_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var _ = Describe("Tenants", func() {
	var aliceClient, bobClient *kubernetes.Clientset

	tenant := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "tenant-get-owned-by-alice",
			Labels: e2eLabels(),
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulerbac.OwnerListSpec{
				{
					CoreOwnerSpec: capsulerbac.CoreOwnerSpec{
						UserSpec: capsulerbac.UserSpec{
							Name: "alice",
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

		Eventually(func() error {
			tenant.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tenant)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tenant)).Should(Succeed())
	})

	It("Should allow tenant owners to get their tenant by name", func() {
		getTenant := func(clientset *kubernetes.Clientset, name string) error {
			_, err := clientset.RESTClient().
				Get().
				AbsPath("/apis/capsule.clastix.io/v1beta2/tenants/" + name).
				DoRaw(context.Background())

			return err
		}

		Eventually(func() error {
			return getTenant(aliceClient, tenant.GetName())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed(), "Alice should get a tenant she owns by name")

		Eventually(func() error {
			return getTenant(bobClient, tenant.GetName())
		}, defaultTimeoutInterval, defaultPollInterval).Should(HaveOccurred(), "Bob should not get a tenant he does not own by name")
	})
})
