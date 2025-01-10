//nolint:all
package e2e_test

import (
	"context"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
