// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// ReplacePluralWithKind returns the GroupVersionKind for a given plural name.
func ReplacePluralWithKind(discoveryClient discovery.DiscoveryInterface, gvk *schema.GroupVersionKind) error {
	groupVersion := gvk.Version
	if gvk.Group != "" {
		groupVersion = gvk.Group + "/" + gvk.Version
	}

	resourceList, err := discoveryClient.ServerResourcesForGroupVersion(groupVersion)
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

// GetGVKFromURL extracts the GroupVersionKind from a cluster-scoped collection
// or named-resource URL. Kind contains the plural API resource name until
// ReplacePluralWithKind resolves it through discovery.
func GetGVKFromURL(path string) *schema.GroupVersionKind {
	parts := strings.Split(strings.Trim(path, "/"), "/")

	switch {
	case parts[0] == "api" && (len(parts) == 3 || len(parts) == 4):
		return &schema.GroupVersionKind{
			Version: parts[1],
			Kind:    parts[2],
		}
	case parts[0] == "apis" && (len(parts) == 4 || len(parts) == 5):
		return &schema.GroupVersionKind{
			Group:   parts[1],
			Version: parts[2],
			Kind:    parts[3],
		}
	}

	return nil
}
