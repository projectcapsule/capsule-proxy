//go:build e2e

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
)

// Start an isolated copy of the installed, changed proxy. The source deployment
// and its arguments stay untouched; both flag modes can run in the same suite.
func globalAccessProxy(f *accessFixture, standalone bool) *rest.Config {
	ctx := context.Background()
	selector := os.Getenv("E2E_PROXY_POD_SELECTOR")
	if selector == "" {
		selector = "app.kubernetes.io/name=capsule-proxy"
	}
	pods := &corev1.PodList{}
	Expect(k8sClient.List(ctx, pods, &client.ListOptions{Raw: &metav1.ListOptions{LabelSelector: selector}})).To(Succeed())
	var source *corev1.Pod
	for i := range pods.Items {
		if pods.Items[i].Status.Phase == corev1.PodRunning && pods.Items[i].DeletionTimestamp == nil {
			source = &pods.Items[i]
			break
		}
	}
	Expect(source).NotTo(BeNil(), "a running changed proxy is required")
	name := f.id + "-open"
	if standalone {
		name = f.id + "-standalone"
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: source.Namespace, Labels: map[string]string{accessRunLabel: f.id}}, Spec: *source.Spec.DeepCopy()}
	pod.Spec.NodeName = ""
	pod.Spec.HostNetwork = false
	pod.Spec.DNSPolicy = corev1.DNSClusterFirst
	pod.Spec.Affinity = nil
	for i := range pod.Spec.Containers {
		for j := range pod.Spec.Containers[i].Ports {
			pod.Spec.Containers[i].Ports[j].HostPort = 0
		}
	}
	var container *corev1.Container
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == "capsule-proxy" {
			container = &pod.Spec.Containers[i]
			break
		}
	}
	Expect(container).NotTo(BeNil())
	args := []string{}
	for _, arg := range container.Args {
		if strings.HasPrefix(arg, "--require-capsule-user=") || strings.HasPrefix(arg, "--listening-port=") || strings.HasPrefix(arg, "--ignored-user-group=") {
			continue
		}
		if standalone && strings.HasPrefix(arg, "--capsule-configuration-name=") {
			continue
		}
		args = append(args, arg)
	}
	container.Args = append(args, "--require-capsule-user=false", "--listening-port=9001", "--ignored-user-group="+f.id+"-ignored", "--ignored-username="+f.id+"-ignored-name")
	if standalone {
		container.Args = append(container.Args, "--capsule-configuration-name="+f.id+"-absent")
	}
	Expect(k8sClient.Create(ctx, pod)).To(Succeed())
	admin, err := kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		if CurrentSpecReport().Failed() {
			logs, err := admin.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: "capsule-proxy"}).DoRaw(ctx)
			if err == nil {
				fmt.Fprintf(GinkgoWriter, "%s logs:\n%s\n", pod.Name, logs)
			}
		}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, pod))).To(Succeed())
	})
	Eventually(func(g Gomega) {
		current := &corev1.Pod{}
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), current)).To(Succeed())
		g.Expect(current.Status.Phase).To(Equal(corev1.PodRunning))
		g.Expect(current.Status.ContainerStatuses).NotTo(BeEmpty())
		for _, status := range current.Status.ContainerStatuses {
			g.Expect(status.Ready).To(BeTrue(), "%s", current.Status.String())
		}
	}, 3*time.Minute, defaultPollInterval).Should(Succeed())
	roundTripper, upgrader, err := spdy.RoundTripperFor(cfg)
	Expect(err).NotTo(HaveOccurred())
	u, err := url.Parse(cfg.Host)
	Expect(err).NotTo(HaveOccurred())
	u.Path = "/api/v1/namespaces/" + pod.Namespace + "/pods/" + pod.Name + "/portforward"
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, u)
	stop, ready := make(chan struct{}), make(chan struct{})
	forwarder, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, []string{"0:9001"}, stop, ready, io.Discard, io.Discard)
	Expect(err).NotTo(HaveOccurred())
	finished := make(chan error, 1)
	go func() { finished <- forwarder.ForwardPorts() }()
	DeferCleanup(func() { close(stop); Eventually(finished, 10*time.Second).Should(Receive()) })
	Eventually(ready, 30*time.Second).Should(BeClosed())
	ports, err := forwarder.GetPorts()
	Expect(err).NotTo(HaveOccurred())
	c := rest.CopyConfig(f.proxyConfig)
	endpoint, err := url.Parse(c.Host)
	Expect(err).NotTo(HaveOccurred())
	c.Host = fmt.Sprintf("%s://127.0.0.1:%d", endpoint.Scheme, ports[0].Local)
	return c
}

func externalAccessClient(base *rest.Config, username string, groups ...string) dynamic.Interface {
	c := rest.CopyConfig(base)
	c.Impersonate = rest.ImpersonationConfig{UserName: username, Groups: append([]string{"system:authenticated"}, groups...)}
	cl, err := dynamic.NewForConfig(c)
	Expect(err).NotTo(HaveOccurred())
	return cl
}

var _ = Describe("Global grants for non-Capsule identities", Ordered, Serial, ContinueOnFailure, Label("global-access"), func() {
	ctx := context.Background()
	var f *accessFixture
	var endpoints []*rest.Config
	var setting *v1beta1.GlobalProxySettings
	var external, other, ignored []dynamic.Interface
	namespaces := schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	clusterRoles := schema.GroupVersionResource{Group: rbacv1.GroupName, Version: "v1", Resource: "clusterroles"}
	listNames := func(cl dynamic.Interface, resource schema.GroupVersionResource, options metav1.ListOptions) ([]string, error) {
		list, err := cl.Resource(resource).List(ctx, options)
		if err != nil {
			return nil, err
		}
		names := []string{}
		for _, item := range list.Items {
			names = append(names, item.GetName())
		}
		return names, nil
	}
	BeforeAll(func() {
		f = newAccessFixture()
		f.addTenant("a", capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, f.subjects["owner"].username, "admin")}, "a")
		f.addTenant("b", capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, f.subjects["other"].username, "admin")}, "b")
		f.create(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "external-reader", Namespace: f.namespaces["b"]}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"list"}}}})
		f.create(&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "external-reader", Namespace: f.namespaces["b"], Labels: map[string]string{"reflection.proxy.projectcapsule.dev/enabled": "true"}}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "external-reader"}, Subjects: []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "Group", Name: f.id + "-external-team"}}})
		configuration := &capsulev1beta2.CapsuleConfiguration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "default"}, configuration)).To(Succeed())
		Expect(configuration.Status.Users.GetByKinds([]capsulerbac.OwnerKind{capsulerbac.GroupOwner})).NotTo(ContainElement(f.id + "-external-team"))
		setting = &v1beta1.GlobalProxySettings{ObjectMeta: metav1.ObjectMeta{Name: f.id + "-global"}, Spec: v1beta1.GlobalProxySettingsSpec{Rules: []v1beta1.GlobalSubjectSpec{{
			Subjects: []v1beta1.GlobalSubject{
				{Kind: capsulerbac.GroupOwner, Name: f.id + "-external-team"},
				{Kind: capsulerbac.UserOwner, Name: f.id + "-external-user"},
				{Kind: capsulerbac.ServiceAccountOwner, Name: f.subjects["serviceaccount"].username},
			},
			ClusterResources: []v1beta1.ClusterResource{{APIGroups: []string{""}, Resources: []string{"namespaces"}, Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: corev1.LabelMetadataName, Operator: metav1.LabelSelectorOpIn, Values: []string{f.namespaces["a"]}}}}}},
		}}}}
		f.create(setting)
		// A user in the grant remains outside Capsule when the flag is omitted.
		closed := externalAccessClient(f.proxyConfig, f.id+"-external", f.id+"-external-team")
		_, err := closed.Resource(namespaces).List(ctx, metav1.ListOptions{})
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "default identity gate must remain enabled: %v", err)
		for _, standalone := range []bool{false, true} {
			endpoint := globalAccessProxy(f, standalone)
			endpoints = append(endpoints, endpoint)
			external = append(external, externalAccessClient(endpoint, f.id+"-external", f.id+"-external-team"))
			other = append(other, externalAccessClient(endpoint, f.id+"-other-external", f.id+"-other-team"))
			ignored = append(ignored, externalAccessClient(endpoint, f.id+"-ignored-user", f.id+"-external-team", f.id+"-ignored"))
		}
		capsuleGroups := configuration.Status.Users.GetByKinds([]capsulerbac.OwnerKind{capsulerbac.GroupOwner})
		Expect(capsuleGroups).NotTo(BeEmpty())
		capsuleGroup := capsuleGroups[0]
		control := externalAccessClient(endpoints[0], f.id+"-external", f.id+"-external-team", capsuleGroup)
		Eventually(func() ([]string, error) { return listNames(control, namespaces, metav1.ListOptions{}) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.namespaces["a"], f.namespaces["b"]), "Capsule callers retain reflection; non-Capsule callers must receive only their global selection")
	})
	It("enforces exact namespace selection, subjects, native RBAC fallback, and advertised access", func() {
		for i, cl := range external {
			By(fmt.Sprintf("checking endpoint %d", i))
			Eventually(func() ([]string, error) { return listNames(cl, namespaces, metav1.ListOptions{}) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.namespaces["a"]))
			for _, username := range []string{f.id + "-external-user", f.subjects["serviceaccount"].username} {
				actor := externalAccessClient(endpoints[i], username)
				Eventually(func() ([]string, error) { return listNames(actor, namespaces, metav1.ListOptions{}) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.namespaces["a"]))
			}
			_, err := cl.Resource(namespaces).Get(ctx, f.namespaces["a"], metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			watcher, watchErr := cl.Resource(namespaces).Watch(ctx, metav1.ListOptions{})
			if watcher != nil {
				watcher.Stop()
			}
			Expect(apierrors.IsForbidden(watchErr)).To(BeTrue(), "global grants do not permit WATCH: %v", watchErr)
			_, err = cl.Resource(namespaces).Get(ctx, f.namespaces["b"], metav1.GetOptions{})
			Expect(err).To(HaveOccurred(), "tenant B must remain inaccessible")
			err = cl.Resource(namespaces).Delete(ctx, f.namespaces["a"], metav1.DeleteOptions{})
			Expect(apierrors.IsForbidden(err)).To(BeTrue(), "global grants do not permit mutations: %v", err)
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: f.namespaces["a"]}, &corev1.Namespace{})).To(Succeed())
			for _, native := range []dynamic.Interface{other[i], ignored[i]} {
				_, err := native.Resource(namespaces).List(ctx, metav1.ListOptions{})
				Expect(apierrors.IsForbidden(err)).To(BeTrue(), "unmatched and ignored subjects retain native denial: %v", err)
			}
			ignoredName := externalAccessClient(endpoints[i], f.id+"-ignored-name", f.id+"-external-team")
			_, err = ignoredName.Resource(namespaces).List(ctx, metav1.ListOptions{})
			Expect(apierrors.IsForbidden(err)).To(BeTrue(), "ignored usernames retain native denial: %v", err)
			_, err = cl.Resource(clusterRoles).List(ctx, metav1.ListOptions{})
			Expect(apierrors.IsForbidden(err)).To(BeTrue(), "ungranted resource must retain native denial: %v", err)
			c := rest.CopyConfig(endpoints[i])
			c.Impersonate = rest.ImpersonationConfig{UserName: f.id + "-external", Groups: []string{"system:authenticated", f.id + "-external-team"}}
			kube, err := kubernetes.NewForConfig(c)
			Expect(err).NotTo(HaveOccurred())
			for _, resource := range []string{"namespaces", "pods"} {
				review, err := kube.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{Resource: resource, Verb: "list"}}}, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
				Expect(review.Status.Allowed).To(Equal(resource == "namespaces"))
			}
			for _, name := range []string{f.namespaces["a"], f.namespaces["b"]} {
				review, err := kube.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{Resource: "namespaces", Verb: "get", Name: name}}}, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
				Expect(review.Status.Allowed).To(Equal(name == f.namespaces["a"]), "named review for %s", name)
			}
			rules, err := kube.AuthorizationV1().SelfSubjectRulesReviews().Create(ctx, &authorizationv1.SelfSubjectRulesReview{Spec: authorizationv1.SelfSubjectRulesReviewSpec{Namespace: f.namespaces["a"]}}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			var namespaceList, podList bool
			for _, rule := range rules.Status.ResourceRules {
				for _, group := range rule.APIGroups {
					for _, resource := range rule.Resources {
						for _, verb := range rule.Verbs {
							if group == "" && verb == "list" {
								namespaceList = namespaceList || resource == "namespaces"
								podList = podList || resource == "pods"
							}
						}
					}
				}
			}
			Expect(namespaceList).To(BeTrue())
			Expect(podList).To(BeFalse())
		}
	})
	It("keeps get-only grants and unrelated operations separate", func() {
		f.create(&v1beta1.GlobalProxySettings{ObjectMeta: metav1.ObjectMeta{Name: f.id + "-get-only"}, Spec: v1beta1.GlobalProxySettingsSpec{Rules: []v1beta1.GlobalSubjectSpec{{
			Subjects:         []v1beta1.GlobalSubject{{Kind: capsulerbac.UserOwner, Name: f.id + "-get-only"}},
			ClusterResources: []v1beta1.ClusterResource{{APIGroups: []string{""}, Resources: []string{"namespaces"}, Operations: []v1beta1.ClusterResourceOperation{v1beta1.ClusterResourceOperationGet}, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: f.namespaces["b"]}}}},
		}}}})
		for _, endpoint := range endpoints {
			actor := externalAccessClient(endpoint, f.id+"-get-only")
			Eventually(func() error {
				_, err := actor.Resource(namespaces).Get(ctx, f.namespaces["b"], metav1.GetOptions{})
				return err
			}, accessTimeout, defaultPollInterval).Should(Succeed())
			_, err := actor.Resource(namespaces).List(ctx, metav1.ListOptions{})
			Expect(apierrors.IsForbidden(err)).To(BeTrue(), "get-only must not grant LIST: %v", err)
		}
	})
	It("uses global namespace access without tenant ownership", func() {
		shared := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: f.id + "-shared"}}
		f.create(shared)
		f.create(&v1beta1.GlobalProxySettings{ObjectMeta: metav1.ObjectMeta{Name: f.id + "-shared"}, Spec: v1beta1.GlobalProxySettingsSpec{Rules: []v1beta1.GlobalSubjectSpec{{
			Subjects:         []v1beta1.GlobalSubject{{Kind: capsulerbac.UserOwner, Name: f.id + "-shared-reader"}},
			ClusterResources: []v1beta1.ClusterResource{{APIGroups: []string{""}, Resources: []string{"namespaces"}, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: shared.Name}}}},
		}}}})
		for _, endpoint := range endpoints {
			actor := externalAccessClient(endpoint, f.id+"-shared-reader")
			Eventually(func() ([]string, error) { return listNames(actor, namespaces, metav1.ListOptions{}) }, accessTimeout, defaultPollInterval).Should(ConsistOf(shared.Name))
			obj, err := actor.Resource(namespaces).Get(ctx, shared.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(obj.GetOwnerReferences()).To(BeEmpty())
			_, err = actor.Resource(namespaces).Get(ctx, f.namespaces["b"], metav1.GetOptions{})
			Expect(apierrors.IsForbidden(err)).To(BeTrue(), "unselected tenant B retains native denial: %v", err)
		}
	})
	It("limits Tenant access to the explicit global selector", func() {
		// This exact name reproduces the previous synthetic tenant identifier.
		// The serial test creates it itself and never adopts an existing Tenant.
		f.create(&capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "global", Labels: map[string]string{corev1.LabelMetadataName: "global"}}, Spec: capsulev1beta2.TenantSpec{Owners: capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, f.subjects["other"].username)}}})
		f.create(&v1beta1.GlobalProxySettings{ObjectMeta: metav1.ObjectMeta{Name: f.id + "-tenants"}, Spec: v1beta1.GlobalProxySettingsSpec{Rules: []v1beta1.GlobalSubjectSpec{{
			Subjects:         []v1beta1.GlobalSubject{{Kind: capsulerbac.UserOwner, Name: f.id + "-tenant-reader"}, {Kind: capsulerbac.UserOwner, Name: f.subjects["coowner"].username}},
			ClusterResources: []v1beta1.ClusterResource{{APIGroups: []string{"capsule.clastix.io"}, Resources: []string{"tenants"}, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: f.tenants["a"].Name}}}},
		}}}})
		resource := schema.GroupVersionResource{Group: "capsule.clastix.io", Version: "v1beta2", Resource: "tenants"}
		for _, actor := range []dynamic.Interface{externalAccessClient(endpoints[0], f.id+"-tenant-reader"), f.subjects["coowner"].client} {
			Eventually(func() ([]string, error) { return listNames(actor, resource, metav1.ListOptions{}) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.tenants["a"].Name))
			_, err := actor.Resource(resource).Get(ctx, f.tenants["a"].Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			for _, name := range []string{f.tenants["b"].Name, "global"} {
				_, err := actor.Resource(resource).Get(ctx, name, metav1.GetOptions{})
				Expect(apierrors.IsNotFound(err) || apierrors.IsForbidden(err)).To(BeTrue(), "unselected tenant %s: %v", name, err)
			}
		}
	})
	It("updates selectors and revokes global grants without losing native forwarding", func() {
		updateSelection := func(name string) {
			Eventually(func() error {
				current := &v1beta1.GlobalProxySettings{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(setting), current); err != nil {
					return err
				}
				current.Spec.Rules[0].ClusterResources[0].Selector.MatchExpressions[0].Values = []string{name}
				return k8sClient.Update(ctx, current)
			}, accessTimeout, defaultPollInterval).Should(Succeed())
		}
		updateSelection(f.namespaces["b"])
		for _, cl := range external {
			Eventually(func() ([]string, error) { return listNames(cl, namespaces, metav1.ListOptions{}) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.namespaces["b"]))
		}
		updateSelection(f.id + "-absent")
		for _, cl := range external {
			Eventually(func() ([]string, error) { return listNames(cl, namespaces, metav1.ListOptions{}) }, accessTimeout, defaultPollInterval).Should(BeEmpty())
		}
		Expect(k8sClient.Delete(ctx, setting)).To(Succeed())
		for _, cl := range external {
			Eventually(func() bool {
				_, err := cl.Resource(namespaces).List(ctx, metav1.ListOptions{})
				return apierrors.IsForbidden(err)
			}, accessTimeout, defaultPollInterval).Should(BeTrue())
		}
	})
	It("preserves successful native RBAC and records direct and proxied latency", func() {
		role := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: f.id + "-native"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"namespaces"}, Verbs: []string{"list"}}}}
		f.create(role)
		f.create(&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: f.id + "-native"}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: role.Name}, Subjects: []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "User", Name: f.id + "-native-reader"}}})
		options := f.listOptions()
		for _, endpoint := range append([]*rest.Config{cfg}, endpoints...) {
			cl := externalAccessClient(endpoint, f.id+"-native-reader")
			Eventually(func() ([]string, error) { return listNames(cl, namespaces, options) }, accessTimeout, defaultPollInterval).Should(ConsistOf(f.namespaces["a"], f.namespaces["b"]))
			start := time.Now()
			const requests = 30
			for range requests {
				names, err := listNames(cl, namespaces, options)
				Expect(err).NotTo(HaveOccurred())
				Expect(names).To(ConsistOf(f.namespaces["a"], f.namespaces["b"]))
			}
			elapsed := time.Since(start)
			fmt.Fprintf(GinkgoWriter, "native namespace LIST endpoint=%s requests=%d mean=%s throughput=%.1f/s\n", endpoint.Host, requests, elapsed/requests, float64(requests)/elapsed.Seconds())
		}
		f.create(&v1beta1.GlobalProxySettings{ObjectMeta: metav1.ObjectMeta{Name: f.id + "-performance"}, Spec: v1beta1.GlobalProxySettingsSpec{Rules: []v1beta1.GlobalSubjectSpec{{
			Subjects:         []v1beta1.GlobalSubject{{Kind: capsulerbac.UserOwner, Name: f.id + "-native-reader"}},
			ClusterResources: []v1beta1.ClusterResource{{APIGroups: []string{""}, Resources: []string{"namespaces"}, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: f.namespaces["a"]}}}},
		}}}})
		options.FieldSelector = "metadata.name=" + f.namespaces["a"]
		for _, endpoint := range append([]*rest.Config{cfg}, endpoints...) {
			cl := externalAccessClient(endpoint, f.id+"-native-reader")
			expected := []string{f.namespaces["a"]}
			if endpoint == cfg {
				expected = append(expected, f.namespaces["b"])
			}
			Eventually(func() ([]string, error) { return listNames(cl, namespaces, f.listOptions()) }, accessTimeout, defaultPollInterval).Should(ConsistOf(expected))
			start := time.Now()
			const requests = 30
			for range requests {
				names, err := listNames(cl, namespaces, options)
				Expect(err).NotTo(HaveOccurred())
				Expect(names).To(ConsistOf(f.namespaces["a"]))
			}
			elapsed := time.Since(start)
			fmt.Fprintf(GinkgoWriter, "selected namespace LIST endpoint=%s requests=%d mean=%s throughput=%.1f/s\n", endpoint.Host, requests, elapsed/requests, float64(requests)/elapsed.Seconds())
		}
	})
})
