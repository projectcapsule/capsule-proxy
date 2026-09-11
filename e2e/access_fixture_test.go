//go:build e2e

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const accessTimeout = 60 * time.Second
const accessRunLabel = "e2e.proxy.projectcapsule.dev/run"
const accessVariantLabel = "e2e.proxy.projectcapsule.dev/variant"

type accessSubject struct {
	username string
	groups   []string
	client   dynamic.Interface
}

type accessFixture struct {
	id          string
	proxyConfig *rest.Config
	namespaces  map[string]string
	tenants     map[string]*capsulev1beta2.Tenant
	subjects    map[string]accessSubject
}

// Requests use the admin kubeconfig's credentials to ask the real proxy to
// impersonate distinct test subjects. The proxy performs real Kubernetes
// impersonation reviews; no authorization or controller behavior is mocked.
// Only the endpoint and server CA are taken from the proxy kubeconfig.
func accessProxyConfig() *rest.Config {
	c := rest.CopyConfig(cfg)
	if endpoint := os.Getenv("E2E_PROXY_URL"); endpoint != "" {
		c.Host = endpoint
		c.TLSClientConfig.CAFile = os.Getenv("E2E_PROXY_CA_FILE")
		c.TLSClientConfig.CAData = nil
		c.TLSClientConfig.ServerName = ""
		c.TLSClientConfig.Insecure = false
	} else {
		proxyConfig, err := clientcmd.BuildConfigFromFlags("", filepath.Join("..", "hack", "alice.kubeconfig"))
		Expect(err).NotTo(HaveOccurred(), "run make generate-kubeconfigs, or set E2E_PROXY_URL and E2E_PROXY_CA_FILE")
		c.Host = proxyConfig.Host
		c.CAFile, c.CAData = proxyConfig.CAFile, proxyConfig.CAData
		c.ServerName, c.Insecure = proxyConfig.ServerName, proxyConfig.Insecure
	}
	c.Timeout = 10 * time.Second
	return c
}

func newAccessFixture() *accessFixture {
	f := &accessFixture{
		id: "proxy-e2e-" + rand.String(8), proxyConfig: accessProxyConfig(),
		namespaces: map[string]string{}, tenants: map[string]*capsulev1beta2.Tenant{}, subjects: map[string]accessSubject{},
	}
	f.addSubject("owner", f.id+"-owner")
	f.addSubject("coowner", f.id+"-coowner")
	f.addSubject("other", f.id+"-other")
	f.addSubject("outsider", f.id+"-outsider")
	f.addSubject("group", f.id+"-member", f.id+"-team")
	f.addSubject("group-name-user", f.id+"-team")
	f.addSubject("limited", f.id+"-limited")
	f.addSubject("empty", f.id+"-empty")
	// A real ServiceAccount object also distinguishes namespace/name matching
	// from a User subject or a same-named ServiceAccount in another namespace.
	control := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: f.id + "-identities"}}
	f.create(control)
	f.namespaces["identities"] = control.Name
	f.create(&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "reader", Namespace: control.Name}})
	f.addSubject("serviceaccount", "system:serviceaccount:"+control.Name+":reader", "system:serviceaccounts", "system:serviceaccounts:"+control.Name)
	f.addSubject("wrong-serviceaccount", "system:serviceaccount:default:reader", "system:serviceaccounts", "system:serviceaccounts:default")
	return f
}

func (f *accessFixture) addSubject(key, username string, groups ...string) {
	c := rest.CopyConfig(f.proxyConfig)
	c.Impersonate = rest.ImpersonationConfig{UserName: username, Groups: append([]string{"system:authenticated", "projectcapsule.dev"}, groups...)}
	cl, err := dynamic.NewForConfig(c)
	Expect(err).NotTo(HaveOccurred())
	f.subjects[key] = accessSubject{username: username, groups: c.Impersonate.Groups, client: cl}
}

func (f *accessFixture) create(obj client.Object) {
	Expect(k8sClient.Create(context.Background(), obj)).To(Succeed(), "create %T %s/%s", obj, obj.GetNamespace(), obj.GetName())
	if obj.GetNamespace() != "" {
		return
	} // The owning test Namespace cleans these up.
	DeferCleanup(func() {
		Eventually(func() error { return client.IgnoreNotFound(k8sClient.Delete(context.Background(), obj)) }, accessTimeout, defaultPollInterval).Should(Succeed())
		Eventually(func() bool {
			err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(obj), obj.DeepCopyObject().(client.Object))
			return apierrors.IsNotFound(err)
		}, 2*time.Minute, defaultPollInterval).Should(BeTrue(), "test resource %s must be deleted", obj.GetName())
	})
}

func (f *accessFixture) owner(kind capsulerbac.OwnerKind, name string, roles ...string) capsulerbac.OwnerSpec {
	return capsulerbac.OwnerSpec{CoreOwnerSpec: capsulerbac.CoreOwnerSpec{
		UserSpec: capsulerbac.UserSpec{Kind: kind, Name: name}, ClusterRoles: roles,
	}}
}

func (f *accessFixture) addTenant(key string, owners capsulerbac.OwnerListSpec, namespaceKeys ...string) {
	tenant := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: f.id + "-" + key}, Spec: capsulev1beta2.TenantSpec{Owners: owners, ForceTenantPrefix: ptr.To(true)}}
	f.create(tenant)
	f.tenants[key] = tenant
	for _, nsKey := range namespaceKeys {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: tenant.Name + "-" + nsKey, Labels: map[string]string{capsulemeta.TenantLabel: tenant.Name, accessRunLabel: f.id}}}
		// Use an owner so Capsule admission follows the same namespace assignment
		// path as tenant users. Register cleanup as soon as creation succeeds.
		NamespaceCreation(ns, owners[0], accessTimeout).Should(Succeed())
		f.namespaces[nsKey] = ns.Name
		DeferCleanup(func() {
			Eventually(func() error { return client.IgnoreNotFound(k8sClient.Delete(context.Background(), ns)) }, accessTimeout, defaultPollInterval).Should(Succeed())
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(ns), &corev1.Namespace{}))
			}, 2*time.Minute, defaultPollInterval).Should(BeTrue())
		})
	}
	Eventually(func(g Gomega) {
		current := &capsulev1beta2.Tenant{}
		g.Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(tenant), current)).To(Succeed())
		g.Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))
		g.Expect(current.Status.Namespaces).To(HaveLen(len(namespaceKeys)))
	}, accessTimeout, defaultPollInterval).Should(Succeed())
}

func (f *accessFixture) waitPermission(subject, namespace, group, resource string, allowed bool) {
	Eventually(func(g Gomega) {
		identity := f.subjects[subject]
		review := &authorizationv1.SubjectAccessReview{Spec: authorizationv1.SubjectAccessReviewSpec{
			User: identity.username, Groups: identity.groups,
			ResourceAttributes: &authorizationv1.ResourceAttributes{Namespace: f.namespaces[namespace], Verb: "list", Group: group, Resource: resource},
		}}
		g.Expect(k8sClient.Create(context.Background(), review)).To(Succeed())
		g.Expect(review.Status.Allowed).To(Equal(allowed))
	}, accessTimeout, defaultPollInterval).Should(Succeed())
}

type accessResource struct {
	group, version, resource, kind string
	fields                         map[string]any
}

func (r accessResource) gvr() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: r.group, Version: r.version, Resource: r.resource}
}

func accessResources() []accessResource {
	container := map[string]any{"name": "pause", "image": "registry.k8s.io/pause:3.10"}
	template := map[string]any{"metadata": map[string]any{"labels": map[string]any{"app": "proxy-e2e"}}, "spec": map[string]any{"containers": []any{container}}}
	return []accessResource{
		{"", "v1", "pods", "Pod", map[string]any{"spec": map[string]any{"containers": []any{container}, "nodeSelector": map[string]any{"e2e.proxy.projectcapsule.dev/unscheduled": "true"}}}},
		{"", "v1", "configmaps", "ConfigMap", map[string]any{"data": map[string]any{"key": "value"}}},
		{"", "v1", "secrets", "Secret", map[string]any{"stringData": map[string]any{"key": "test-value"}}},
		{"", "v1", "services", "Service", map[string]any{"spec": map[string]any{"ports": []any{map[string]any{"port": int64(80)}}}}},
		{"", "v1", "serviceaccounts", "ServiceAccount", nil},
		{"", "v1", "persistentvolumeclaims", "PersistentVolumeClaim", map[string]any{"spec": map[string]any{"storageClassName": "", "accessModes": []any{"ReadWriteOnce"}, "resources": map[string]any{"requests": map[string]any{"storage": "1Mi"}}}}},
		{"apps", "v1", "deployments", "Deployment", map[string]any{"spec": map[string]any{"replicas": int64(0), "selector": map[string]any{"matchLabels": map[string]any{"app": "proxy-e2e"}}, "template": template}}},
		{"apps", "v1", "statefulsets", "StatefulSet", map[string]any{"spec": map[string]any{"replicas": int64(0), "serviceName": "sample", "selector": map[string]any{"matchLabels": map[string]any{"app": "proxy-e2e"}}, "template": template}}},
		{"batch", "v1", "cronjobs", "CronJob", map[string]any{"spec": map[string]any{"schedule": "0 0 * * *", "suspend": true, "jobTemplate": map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{"restartPolicy": "Never", "containers": []any{container}}}}}}}},
		{"networking.k8s.io", "v1", "networkpolicies", "NetworkPolicy", map[string]any{"spec": map[string]any{"podSelector": map[string]any{}, "policyTypes": []any{"Ingress"}, "ingress": []any{map[string]any{}}}}},
		{"rbac.authorization.k8s.io", "v1", "roles", "Role", map[string]any{"rules": []any{map[string]any{"apiGroups": []any{""}, "resources": []any{"configmaps"}, "verbs": []any{"get"}}}}},
	}
}

func (f *accessFixture) seed(resource accessResource, tenantKey string, nsKeys ...string) {
	for _, nsKey := range nsKeys {
		for _, name := range []string{"sample", "alternate"} {
			obj := &unstructured.Unstructured{Object: map[string]any{}}
			for key, value := range resource.fields {
				obj.Object[key] = value
			}
			obj = obj.DeepCopy()
			obj.SetGroupVersionKind(schema.GroupVersionKind{Group: resource.group, Version: resource.version, Kind: resource.kind})
			obj.SetName(name)
			obj.SetNamespace(f.namespaces[nsKey])
			obj.SetLabels(map[string]string{accessRunLabel: f.id, accessVariantLabel: name, capsulemeta.ManagedByCapsuleLabel: f.tenants[tenantKey].Name})
			f.create(obj)
		}
	}
}

func (f *accessFixture) list(subject string, resource accessResource, namespace string, opts metav1.ListOptions) ([]string, error) {
	list, err := f.subjects[subject].client.Resource(resource.gvr()).Namespace(namespace).List(context.Background(), opts)
	if err != nil {
		return nil, err
	}
	if list.GetKind() != resource.kind+"List" {
		return nil, fmt.Errorf("unexpected list kind %q", list.GetKind())
	}
	ids := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		ids = append(ids, item.GetNamespace()+"/"+item.GetName())
	}
	return ids, nil
}

func (f *accessFixture) listOptions() metav1.ListOptions {
	return metav1.ListOptions{LabelSelector: labels.Set{accessRunLabel: f.id}.String()}
}

func (f *accessFixture) ids(nsKeys []string, names ...string) []string {
	ids := make([]string, 0, len(nsKeys)*len(names))
	for _, nsKey := range nsKeys {
		for _, name := range names {
			ids = append(ids, f.namespaces[nsKey]+"/"+name)
		}
	}
	return ids
}

func (f *accessFixture) expectList(subject string, resource accessResource, nsKeys ...string) {
	Eventually(func() ([]string, error) { return f.list(subject, resource, "", f.listOptions()) }, accessTimeout, defaultPollInterval).
		Should(ConsistOf(f.ids(nsKeys, "sample", "alternate")), "%s listing %s", subject, resource.resource)
}

func reflectionSubject(kind, name, namespace string) rbacv1.Subject {
	group := rbacv1.GroupName
	if kind == rbacv1.ServiceAccountKind {
		group = ""
	}
	return rbacv1.Subject{Kind: kind, APIGroup: group, Name: name, Namespace: namespace}
}
