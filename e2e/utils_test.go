//nolint:all
package e2e_test

import (
	"context"
	"path/filepath"
	"time"

	. "github.com/onsi/gomega"
	capsuleapi "github.com/projectcapsule/capsule/pkg/api"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	defaultTimeoutInterval = 20 * time.Second
	defaultPollInterval    = time.Second
)

// Returns labels to identify e2e resources.
func e2eLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/env": "e2e",
	}
}

// Returns a label selector to filter e2e resources.
func e2eSelector() labels.Selector {
	return labels.SelectorFromSet(e2eLabels())
}

// Pass objects which require cleanup and a label selector to filter them.
func cleanResources(res []client.Object, selector labels.Selector) (err error) {
	for _, resource := range res {
		err = k8sClient.DeleteAllOf(context.TODO(), resource, &client.DeleteAllOfOptions{
			ListOptions: client.ListOptions{
				LabelSelector: selector,
			},
		})

		if err != nil {
			return err
		}
	}

	return nil
}

// LoadKubeConfig loads the kubeconfig for a specific user and returns the Kubernetes client.
func loadKubeConfig(user string) (*kubernetes.Clientset, error) {
	// Adjust the path to your kubeconfigs
	kubeConfigPath := filepath.Join("../hack", user+".kubeconfig")

	// Build the configuration
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		return nil, err
	}

	// Create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return clientset, nil
}

func NewNamespace(name string, labels ...map[string]string) *corev1.Namespace {
	if len(name) == 0 {
		name = rand.String(10)
	}

	var namespaceLabels map[string]string
	if len(labels) > 0 {
		namespaceLabels = labels[0]
	}

	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: namespaceLabels,
		},
	}
}

func NamespaceCreation(ns *corev1.Namespace, owner capsuleapi.OwnerSpec, timeout time.Duration) AsyncAssertion {
	cs := ownerClient(owner)
	return Eventually(func() (err error) {
		_, err = cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		return
	}, timeout, defaultPollInterval)
}

func ownerClient(owner capsuleapi.OwnerSpec) (cs kubernetes.Interface) {
	c, err := config.GetConfig()
	Expect(err).ToNot(HaveOccurred())
	c.Impersonate.Groups = []string{"projectcapsule.dev", owner.Name}
	c.Impersonate.UserName = owner.Name
	cs, err = kubernetes.NewForConfig(c)
	Expect(err).ToNot(HaveOccurred())

	return cs
}
