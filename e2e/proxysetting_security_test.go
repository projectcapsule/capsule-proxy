//go:build e2e

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	extensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
)

var _ = Describe("ProxySetting cluster-resource isolation", Ordered, Serial, ContinueOnFailure, Label("proxysetting-security"), func() {
	var f *accessFixture
	var ownerClient client.Client
	var setting *v1beta1.ProxySetting
	var legacySetting *v1beta1.ProxySetting
	ctx := context.Background()
	forbiddenGrants := func() []v1beta1.ClusterResource {
		return []v1beta1.ClusterResource{{
			APIGroups: []string{"*"}, Resources: []string{"*"},
			Operations: []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationList, v1beta1.ClusterResourceOperationGet},
			Selector:   &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: corev1.LabelMetadataName, Operator: metav1.LabelSelectorOpExists}}},
		}}
	}
	namespaces := schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	clusterRoles := schema.GroupVersionResource{Group: rbacv1.GroupName, Version: "v1", Resource: "clusterroles"}
	listNames := func(subject string, resource schema.GroupVersionResource) ([]string, error) {
		list, err := f.subjects[subject].client.Resource(resource).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(list.Items))
		for _, item := range list.Items {
			names = append(names, item.GetName())
		}
		return names, nil
	}
	BeforeAll(func() {
		f = newAccessFixture()
		f.addTenant("a", capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, f.subjects["owner"].username, "admin")}, "a")
		f.addTenant("b", capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, f.subjects["other"].username, "admin")}, "b")
		f.create(&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: f.id + "-foreign", Labels: map[string]string{corev1.LabelMetadataName: f.id + "-foreign"}}})
		f.create(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "proxysetting-writer", Namespace: f.namespaces["a"]}, Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{v1beta1.GroupVersion.Group}, Resources: []string{"proxysettings"}, Verbs: []string{"create", "get", "list", "update", "patch"},
		}}})
		f.create(&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "proxysetting-writer", Namespace: f.namespaces["a"]},
			RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "proxysetting-writer"},
			Subjects: []rbacv1.Subject{{Kind: "User", APIGroup: rbacv1.GroupName, Name: f.subjects["owner"].username}},
		})
		ownerConfig := rest.CopyConfig(cfg)
		ownerConfig.Impersonate = rest.ImpersonationConfig{UserName: f.subjects["owner"].username, Groups: f.subjects["owner"].groups}
		var err error
		ownerClient, err = client.New(ownerConfig, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		setting = &v1beta1.ProxySetting{ObjectMeta: metav1.ObjectMeta{Name: "delegation", Namespace: f.namespaces["a"]}, Spec: v1beta1.ProxySettingSpec{Subjects: []v1beta1.OwnerSpec{
			{Kind: capsulerbac.UserOwner, Name: f.subjects["owner"].username},
			{Kind: capsulerbac.UserOwner, Name: f.subjects["outsider"].username},
		}}}
		// This positive creation proves rejection cases are exercising schema
		// validation, not a missing tenant owner's RBAC grant.
		Eventually(func() error { return ownerClient.Create(ctx, setting) }, accessTimeout, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &v1beta1.ProxySetting{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(setting), current)).To(Succeed())
			g.Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))
		}, accessTimeout, defaultPollInterval).Should(Succeed())
	})

	It("preserves delegation inside the setting's tenant", func() {
		Eventually(func() ([]string, error) { return listNames("outsider", namespaces) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.namespaces["a"]))
		_, err := f.subjects["outsider"].client.Resource(namespaces).Get(ctx, f.namespaces["b"], metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "foreign tenant namespace must remain hidden: %v", err)
	})

	It("rejects a tenant owner's cluster-resource grant on create", func() {
		malicious := setting.DeepCopy()
		malicious.Name = "self-grant"
		malicious.ResourceVersion = ""
		malicious.UID = ""
		malicious.Spec.Subjects[0].ClusterResources = forbiddenGrants()
		err := ownerClient.Create(ctx, malicious)
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected schema rejection, got %v", err)
		Expect(err.Error()).To(ContainSubstring("spec.subjects[0].clusterResources"))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(malicious), &v1beta1.ProxySetting{}))).To(BeTrue())
	})

	It("rejects adding cluster-resource grants on update and patch", func() {
		current := &v1beta1.ProxySetting{}
		Expect(ownerClient.Get(ctx, client.ObjectKeyFromObject(setting), current)).To(Succeed())
		original := current.DeepCopy()
		current.Spec.Subjects[0].ClusterResources = forbiddenGrants()
		err := ownerClient.Update(ctx, current)
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected schema rejection, got %v", err)
		Expect(err.Error()).To(ContainSubstring("spec.subjects[0].clusterResources"))
		err = ownerClient.Patch(ctx, current, client.MergeFrom(original))
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected schema rejection, got %v", err)
		Expect(err.Error()).To(ContainSubstring("spec.subjects[0].clusterResources"))
		persisted := &v1beta1.ProxySetting{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(setting), persisted)).To(Succeed())
		Expect(persisted.Spec.Subjects[0].ClusterResources).To(BeEmpty())
	})

	It("ignores grants stored before the CRD validation was installed", func() {
		// This serial suite requires a disposable cluster. Temporarily restore
		// the old field schema to seed a real pre-upgrade object, then reinstate
		// validation before exercising the running proxy.
		extensions, err := extensionsclient.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		crds := extensions.ApiextensionsV1().CustomResourceDefinitions()
		const crdName = "proxysettings.capsule.clastix.io"
		crd, err := crds.Get(ctx, crdName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		versionIndex := -1
		for i, version := range crd.Spec.Versions {
			if version.Name == v1beta1.GroupVersion.Version {
				versionIndex = i
				limit := version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["subjects"].Items.Schema.Properties["clusterResources"].MaxItems
				Expect(limit).NotTo(BeNil())
				Expect(*limit).To(BeZero())
			}
		}
		Expect(versionIndex).To(BeNumerically(">=", 0))
		path := fmt.Sprintf("/spec/versions/%d/schema/openAPIV3Schema/properties/spec/properties/subjects/items/properties/clusterResources/maxItems", versionIndex)
		restore := func() error {
			_, err := crds.Patch(ctx, crdName, types.JSONPatchType, []byte(fmt.Sprintf(`[{"op":"add","path":%q,"value":0}]`, path)), metav1.PatchOptions{})
			return err
		}
		DeferCleanup(func() { Eventually(restore, accessTimeout, defaultPollInterval).Should(Succeed()) })
		_, err = crds.Patch(ctx, crdName, types.JSONPatchType, []byte(fmt.Sprintf(`[{"op":"remove","path":%q}]`, path)), metav1.PatchOptions{})
		Expect(err).NotTo(HaveOccurred())
		legacySetting = &v1beta1.ProxySetting{ObjectMeta: metav1.ObjectMeta{Name: "pre-upgrade", Namespace: setting.Namespace}, Spec: *setting.Spec.DeepCopy()}
		for i := range legacySetting.Spec.Subjects {
			legacySetting.Spec.Subjects[i].ClusterResources = forbiddenGrants()
		}
		Eventually(func() error { return ownerClient.Create(ctx, legacySetting) }, accessTimeout, defaultPollInterval).Should(Succeed())
		Expect(restore()).To(Succeed())
		Eventually(func() bool {
			probe := legacySetting.DeepCopy()
			probe.Name, probe.ResourceVersion, probe.UID = "schema-probe", "", ""
			return apierrors.IsInvalid(ownerClient.Create(ctx, probe, client.DryRunAll))
		}, accessTimeout, defaultPollInterval).Should(BeTrue(), "schema must be restored before checking runtime isolation")
		Eventually(func(g Gomega) {
			stored := &v1beta1.ProxySetting{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(legacySetting), stored)).To(Succeed())
			g.Expect(stored.Spec.Subjects[0].ClusterResources).NotTo(BeEmpty())
			g.Expect(stored.Status.ObservedGeneration).To(Equal(stored.Generation))
		}, accessTimeout, defaultPollInterval).Should(Succeed())
		for _, subject := range []string{"owner", "outsider"} {
			Eventually(func() ([]string, error) { return listNames(subject, namespaces) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.namespaces["a"]))
			_, err := f.subjects[subject].client.Resource(namespaces).Get(ctx, f.namespaces["a"], metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			_, err = f.subjects[subject].client.Resource(namespaces).Get(ctx, f.namespaces["b"], metav1.GetOptions{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "stored grant must not expose tenant B: %v", err)
		}
	})

	It("preserves administrator grants through GlobalProxySettings", Label("proxysetting-global"), func() {
		global := &v1beta1.GlobalProxySettings{ObjectMeta: metav1.ObjectMeta{Name: f.id + "-global"}, Spec: v1beta1.GlobalProxySettingsSpec{Rules: []v1beta1.GlobalSubjectSpec{{
			Subjects: []v1beta1.GlobalSubject{{Kind: capsulerbac.UserOwner, Name: f.subjects["coowner"].username}},
			ClusterResources: []v1beta1.ClusterResource{
				{APIGroups: []string{""}, Resources: []string{"namespaces"}, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: f.namespaces["b"]}}},
				{APIGroups: []string{rbacv1.GroupName}, Resources: []string{"clusterroles"}, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: f.id + "-foreign"}}},
			},
		}}}}
		// Tenant-writable settings must not be replaceable by a self-created
		// global grant. Only the administrator fixture creates this object.
		Expect(apierrors.IsForbidden(ownerClient.Create(ctx, global.DeepCopy()))).To(BeTrue())
		f.create(global)
		// This subject owns no Tenant and has no namespaced delegation.
		Eventually(func() ([]string, error) { return listNames("coowner", namespaces) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.namespaces["b"]))
		Eventually(func() ([]string, error) { return listNames("coowner", clusterRoles) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.id + "-foreign"))
		_, err := f.subjects["coowner"].client.Resource(clusterRoles).Get(ctx, f.id+"-foreign", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		// Positive visibility proves the cluster-resource route is ready before
		// checking that stored namespaced grants expose nothing.
		Expect(legacySetting).NotTo(BeNil())
		for _, subject := range []string{"owner", "outsider"} {
			names, err := listNames(subject, clusterRoles)
			Expect(err).NotTo(HaveOccurred())
			Expect(names).To(BeEmpty())
			_, err = f.subjects[subject].client.Resource(clusterRoles).Get(ctx, f.id+"-foreign", metav1.GetOptions{})
			Expect(apierrors.IsForbidden(err)).To(BeTrue(), "GET must retain native RBAC: %v", err)
		}
		// Another delegated subject does not inherit the administrator's grant.
		_, err = f.subjects["outsider"].client.Resource(namespaces).Get(ctx, f.namespaces["b"], metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
		Expect(k8sClient.Delete(ctx, global)).To(Succeed())
		Eventually(func() ([]string, error) { return listNames("coowner", namespaces) }, accessTimeout, defaultPollInterval).Should(BeEmpty())
		Eventually(func() ([]string, error) { return listNames("coowner", clusterRoles) }, accessTimeout, defaultPollInterval).Should(BeEmpty())
	})
})
