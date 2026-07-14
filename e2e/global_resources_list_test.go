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

var _ = Describe("GlobalProxySettings resource lists", func() {
	const selectionLabel = "proxy.projectcapsule.dev/e2e-selection"

	var aliceClient, bobClient *kubernetes.Clientset

	labelsFor := func(selection string) map[string]string {
		labels := e2eLabels()
		labels[selectionLabel] = selection

		return labels
	}

	clusterResource := func(apiGroup string, resources []string, selection string) v1beta1.ClusterResource {
		return v1beta1.ClusterResource{
			APIGroups:  []string{apiGroup},
			Resources:  resources,
			Operations: []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationList},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{selectionLabel: selection},
			},
		}
	}

	BeforeEach(func() {
		settings := &v1beta1.GlobalProxySettings{
			ObjectMeta: metav1.ObjectMeta{Name: "global-resource-list-cases", Labels: e2eLabels()},
			Spec: v1beta1.GlobalProxySettingsSpec{
				Rules: []v1beta1.GlobalSubjectSpec{
					{
						// Explicit resources across three different GVKs.
						ClusterResources: []v1beta1.ClusterResource{
							clusterResource("", []string{"namespaces"}, "first"),
							clusterResource("capsule.clastix.io", []string{"tenants"}, "first"),
							clusterResource("rbac.authorization.k8s.io", []string{"clusterroles"}, "first"),
						},
						Subjects: []v1beta1.GlobalSubject{{Kind: "User", Name: "alice"}},
					},
					{
						// A second selector and wildcard resources prove that any number of
						// resource rules and more than one selector are combined.
						ClusterResources: []v1beta1.ClusterResource{
							clusterResource("", []string{"*"}, "second"),
							clusterResource("capsule.clastix.io", []string{"*"}, "second"),
							clusterResource("rbac.authorization.k8s.io", []string{"*"}, "second"),
						},
						Subjects: []v1beta1.GlobalSubject{{Kind: "User", Name: "alice"}},
					},
					{
						// These selectors match the negative-control objects, but their API
						// groups and resource names are intentionally wrong.
						ClusterResources: []v1beta1.ClusterResource{
							clusterResource("storage.k8s.io", []string{"storageclasses"}, "wrong"),
							clusterResource("", []string{"nodes"}, "wrong"),
							clusterResource("capsule.clastix.io", []string{"globalproxysettings"}, "wrong"),
							clusterResource("rbac.authorization.k8s.io", []string{"clusterrolebindings"}, "wrong"),
						},
						Subjects: []v1beta1.GlobalSubject{{Kind: "User", Name: "bob"}},
					},
					{
						// A correct wildcard rule for the wrong subject must not grant Alice access.
						ClusterResources: []v1beta1.ClusterResource{
							clusterResource("*", []string{"*"}, "wrong"),
						},
						Subjects: []v1beta1.GlobalSubject{{Kind: "User", Name: "somebody-else"}},
					},
				},
			},
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

		var err error
		aliceClient, err = loadKubeConfig("alice")
		Expect(err).NotTo(HaveOccurred())
		bobClient, err = loadKubeConfig("bob")
		Expect(err).NotTo(HaveOccurred())
	})

	JustAfterEach(func() {
		resources := []client.Object{
			&corev1.Namespace{},
			&capsulev1beta2.Tenant{},
			&rbacv1.ClusterRole{},
			&v1beta1.GlobalProxySettings{},
		}
		Eventually(func() error {
			return cleanResources(resources, e2eSelector())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("combines resources and selectors while excluding incorrect rules", func() {
		owner := capsulerbac.OwnerListSpec{{
			CoreOwnerSpec: capsulerbac.CoreOwnerSpec{
				UserSpec: capsulerbac.UserSpec{Name: "global-list-resource-owner", Kind: "User"},
			},
		}}

		tenants := []*capsulev1beta2.Tenant{
			{ObjectMeta: metav1.ObjectMeta{Name: "global-list-tenant-first-a", Labels: labelsFor("first")}, Spec: capsulev1beta2.TenantSpec{Owners: owner}},
			{ObjectMeta: metav1.ObjectMeta{Name: "global-list-tenant-first-b", Labels: labelsFor("first")}, Spec: capsulev1beta2.TenantSpec{Owners: owner}},
			{ObjectMeta: metav1.ObjectMeta{Name: "global-list-tenant-second", Labels: labelsFor("second")}, Spec: capsulev1beta2.TenantSpec{Owners: owner}},
			{ObjectMeta: metav1.ObjectMeta{Name: "global-list-tenant-wrong", Labels: labelsFor("wrong")}, Spec: capsulev1beta2.TenantSpec{Owners: owner}},
			{ObjectMeta: metav1.ObjectMeta{Name: "global-list-tenant-unmatched", Labels: labelsFor("unmatched")}, Spec: capsulev1beta2.TenantSpec{Owners: owner}},
		}
		namespaces := []*corev1.Namespace{
			{ObjectMeta: metav1.ObjectMeta{Name: "global-list-namespace-first-a", Labels: labelsFor("first")}},
			{ObjectMeta: metav1.ObjectMeta{Name: "global-list-namespace-first-b", Labels: labelsFor("first")}},
			{ObjectMeta: metav1.ObjectMeta{Name: "global-list-namespace-second", Labels: labelsFor("second")}},
			{ObjectMeta: metav1.ObjectMeta{Name: "global-list-namespace-wrong", Labels: labelsFor("wrong")}},
			{ObjectMeta: metav1.ObjectMeta{Name: "global-list-namespace-unmatched", Labels: labelsFor("unmatched")}},
		}
		roles := []*rbacv1.ClusterRole{
			{ObjectMeta: metav1.ObjectMeta{Name: "global-list-role-first-a", Labels: labelsFor("first")}},
			{ObjectMeta: metav1.ObjectMeta{Name: "global-list-role-first-b", Labels: labelsFor("first")}},
			{ObjectMeta: metav1.ObjectMeta{Name: "global-list-role-second", Labels: labelsFor("second")}},
			{ObjectMeta: metav1.ObjectMeta{Name: "global-list-role-wrong", Labels: labelsFor("wrong")}},
			{ObjectMeta: metav1.ObjectMeta{Name: "global-list-role-unmatched", Labels: labelsFor("unmatched")}},
		}

		for _, objects := range [][]client.Object{
			{tenants[0], tenants[1], tenants[2], tenants[3], tenants[4]},
			{namespaces[0], namespaces[1], namespaces[2], namespaces[3], namespaces[4]},
			{roles[0], roles[1], roles[2], roles[3], roles[4]},
		} {
			for _, object := range objects {
				object := object
				Eventually(func() error {
					object.SetResourceVersion("")

					return k8sClient.Create(context.Background(), object)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		}

		expectedTenants := []string{tenants[0].Name, tenants[1].Name, tenants[2].Name}
		expectedNamespaces := []string{namespaces[0].Name, namespaces[1].Name, namespaces[2].Name}
		expectedRoles := []string{roles[0].Name, roles[1].Name, roles[2].Name}

		listTenants := func(clientset *kubernetes.Clientset) ([]string, error) {
			raw, err := clientset.RESTClient().Get().AbsPath("/apis/capsule.clastix.io/v1beta2/tenants").DoRaw(context.Background())
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

		listNamespaces := func(clientset *kubernetes.Clientset) ([]string, error) {
			list, err := clientset.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			names := make([]string, 0, len(list.Items))
			for i := range list.Items {
				names = append(names, list.Items[i].Name)
			}

			return names, nil
		}

		listClusterRoles := func(clientset *kubernetes.Clientset) ([]string, error) {
			list, err := clientset.RbacV1().ClusterRoles().List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			names := make([]string, 0, len(list.Items))
			for i := range list.Items {
				names = append(names, list.Items[i].Name)
			}

			return names, nil
		}

		Eventually(func() ([]string, error) { return listTenants(aliceClient) }, defaultTimeoutInterval, defaultPollInterval).
			Should(ConsistOf(expectedTenants))
		Eventually(func() ([]string, error) { return listNamespaces(aliceClient) }, defaultTimeoutInterval, defaultPollInterval).
			Should(ConsistOf(expectedNamespaces))
		Eventually(func() ([]string, error) { return listClusterRoles(aliceClient) }, defaultTimeoutInterval, defaultPollInterval).
			Should(ConsistOf(expectedRoles))

		// Bob only has rules whose GVKs do not match these endpoints. LIST must
		// still succeed; the proxy adds a selector which deliberately matches no
		// objects instead of returning an authorization or routing error.
		Eventually(func() ([]string, error) { return listTenants(bobClient) }, defaultTimeoutInterval, defaultPollInterval).
			Should(BeEmpty())
		Eventually(func() ([]string, error) { return listNamespaces(bobClient) }, defaultTimeoutInterval, defaultPollInterval).
			Should(BeEmpty())
		Eventually(func() ([]string, error) { return listClusterRoles(bobClient) }, defaultTimeoutInterval, defaultPollInterval).
			Should(BeEmpty())
	})
})
