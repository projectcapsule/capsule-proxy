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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
)

var _ = Describe("GlobalProxySettings resource access", func() {
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
	defaultOperations := func(apiGroup string, resources []string, selection string) v1beta1.ClusterResource {
		resource := clusterResource(apiGroup, resources, selection)
		resource.Operations = nil

		return resource
	}
	getResource := func(apiGroup string, resources []string, selection string) v1beta1.ClusterResource {
		resource := clusterResource(apiGroup, resources, selection)
		resource.Operations = []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationGet}

		return resource
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
							defaultOperations("", []string{"persistentvolumes"}, "first"),
							clusterResource("capsule.clastix.io", []string{"tenants"}, "first"),
							defaultOperations("rbac.authorization.k8s.io", []string{"clusterroles"}, "first"),
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
						// Bob has valid rules for all three endpoints, but this selector
						// deliberately matches none of the test resources.
						ClusterResources: []v1beta1.ClusterResource{
							clusterResource("", []string{"namespaces"}, "missing"),
							clusterResource("capsule.clastix.io", []string{"tenants"}, "missing"),
							clusterResource("rbac.authorization.k8s.io", []string{"clusterroles"}, "missing"),
						},
						Subjects: []v1beta1.GlobalSubject{{Kind: "User", Name: "bob"}},
					},
					{
						// Valid but different GVKs must not affect Alice's three lists.
						ClusterResources: []v1beta1.ClusterResource{
							clusterResource("storage.k8s.io", []string{"storageclasses"}, "wrong"),
							clusterResource("", []string{"nodes"}, "wrong"),
							clusterResource("capsule.clastix.io", []string{"globalproxysettings"}, "wrong"),
							clusterResource("rbac.authorization.k8s.io", []string{"clusterrolebindings"}, "wrong"),
						},
						Subjects: []v1beta1.GlobalSubject{{Kind: "User", Name: "alice"}},
					},
					{
						// A correct wildcard rule for the wrong subject must not grant Alice access.
						ClusterResources: []v1beta1.ClusterResource{
							clusterResource("*", []string{"*"}, "wrong"),
						},
						Subjects: []v1beta1.GlobalSubject{{Kind: "User", Name: "somebody-else"}},
					},
					{
						// GET can be enabled independently without including the resource in LIST.
						ClusterResources: []v1beta1.ClusterResource{
							getResource("rbac.authorization.k8s.io", []string{"clusterroles"}, "get-only"),
						},
						Subjects: []v1beta1.GlobalSubject{{Kind: "User", Name: "alice"}},
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
		for _, name := range []string{
			"global-list-namespace-first-a",
			"global-list-namespace-first-b",
			"global-list-namespace-second",
			"global-list-namespace-wrong",
			"global-list-namespace-unmatched",
		} {
			name := name
			Eventually(func() error {
				return client.IgnoreNotFound(k8sClient.Delete(context.Background(), &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: name},
				}))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		resources := []client.Object{
			&corev1.PersistentVolume{},
			&capsulev1beta2.Tenant{},
			&rbacv1.ClusterRole{},
			&v1beta1.GlobalProxySettings{},
		}
		Eventually(func() error {
			return cleanResources(resources, e2eSelector())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("applies default and explicit LIST/GET operations without crossing selectors", func() {
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
			{ObjectMeta: metav1.ObjectMeta{Name: "global-get-role", Labels: labelsFor("get-only")}},
		}
		persistentVolume := &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "global-get-persistent-volume", Labels: labelsFor("first")},
			Spec: corev1.PersistentVolumeSpec{
				Capacity:                      corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
				AccessModes:                   []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
				PersistentVolumeSource: corev1.PersistentVolumeSource{
					HostPath: &corev1.HostPathVolumeSource{Path: "/tmp/global-get-persistent-volume"},
				},
			},
		}

		for _, objects := range [][]client.Object{
			{tenants[0], tenants[1], tenants[2], tenants[3], tenants[4]},
			{namespaces[0], namespaces[1], namespaces[2], namespaces[3], namespaces[4]},
			{roles[0], roles[1], roles[2], roles[3], roles[4], roles[5]},
			{persistentVolume},
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

		// Bob has rules for these GVKs whose selector matches no objects. LIST must
		// still succeed; the proxy adds a selector which deliberately matches no
		// objects instead of returning an authorization or routing error.
		Eventually(func() ([]string, error) { return listTenants(bobClient) }, defaultTimeoutInterval, defaultPollInterval).
			Should(BeEmpty())
		Eventually(func() ([]string, error) { return listNamespaces(bobClient) }, defaultTimeoutInterval, defaultPollInterval).
			Should(BeEmpty())
		Eventually(func() ([]string, error) { return listClusterRoles(bobClient) }, defaultTimeoutInterval, defaultPollInterval).
			Should(BeEmpty())

		// Omitted operations default to LIST and GET.
		_, err := aliceClient.RbacV1().ClusterRoles().Get(context.Background(), roles[0].Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		// An explicit GET-only rule permits a named read but does not leak the
		// object into the collection result asserted above.
		_, err = aliceClient.RbacV1().ClusterRoles().Get(context.Background(), roles[5].Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		// Existing v1beta1 settings commonly contain an explicit LIST because it
		// was the only supported value. It must continue to authorize named GETs.
		_, err = aliceClient.RbacV1().ClusterRoles().Get(context.Background(), roles[2].Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		// Core named-resource paths use /api/{version}/{resource}/{name}; this
		// protects the persistent-volume regression where that path was parsed as
		// a grouped API and fell through to the subject's native RBAC.
		_, err = aliceClient.CoreV1().PersistentVolumes().Get(context.Background(), persistentVolume.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
	})
})
