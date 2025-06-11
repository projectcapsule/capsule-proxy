// Copyright 2020-2025 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// GetGVKByPlural returns the GroupVersionKind for a given plural name.
func ReplacePluralWithKind(discoveryClient *discovery.DiscoveryClient, gvk *schema.GroupVersionKind) error {
	resourceList, err := discoveryClient.ServerResourcesForGroupVersion(gvk.Group + "/" + gvk.Version)
	if err != nil {
		return err
	}

	for _, resource := range resourceList.APIResources {
		if resource.Name == gvk.Kind {
			gvk.Kind = resource.Kind

			return nil
		}
	}

	return fmt.Errorf("could not find GVK for plural name: %s", gvk.Kind)
}

// Since the URL is in the format /apis/{group}/{version}/{kind} or /api/{version}/{kind}, we can extract the GVK from the URL.
// However the kind will be the plural form.
func GetGVKFromURL(url string) *schema.GroupVersionKind {
	parts := strings.Split(url, "/")

	switch len(parts) {
	case 5, 6:
		return &schema.GroupVersionKind{
			Group:   parts[2],
			Version: parts[3],
			Kind:    parts[4],
		}
	case 4:
		return &schema.GroupVersionKind{
			Version: parts[2],
			Kind:    parts[3],
		}
	}

	return nil
}
