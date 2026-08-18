// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestGetGVKFromURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want *schema.GroupVersionKind
	}{
		{name: "core collection", path: "/api/v1/persistentvolumes", want: &schema.GroupVersionKind{Version: "v1", Kind: "persistentvolumes"}},
		{name: "core named resource", path: "/api/v1/persistentvolumes/pvc-0574499c-f01b-4a53-85f1-ddb002de9cae", want: &schema.GroupVersionKind{Version: "v1", Kind: "persistentvolumes"}},
		{name: "grouped collection", path: "/apis/rbac.authorization.k8s.io/v1/clusterroles", want: &schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "clusterroles"}},
		{name: "grouped named resource", path: "/apis/rbac.authorization.k8s.io/v1/clusterroles/view", want: &schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "clusterroles"}},
		{name: "core subresource", path: "/api/v1/nodes/worker/status"},
		{name: "invalid", path: "/healthz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := GetGVKFromURL(tt.path)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("GetGVKFromURL(%q)=%v, want nil", tt.path, got)
				}

				return
			}

			if got == nil || *got != *tt.want {
				t.Fatalf("GetGVKFromURL(%q)=%v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestReplacePluralWithKindUsesCanonicalCoreGroupVersion(t *testing.T) {
	t.Parallel()

	discovery := &fake.FakeDiscovery{Fake: &clienttesting.Fake{}}
	discovery.Resources = []*metav1.APIResourceList{{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{{Name: "persistentvolumes", Kind: "PersistentVolume"}},
	}}
	gvk := &schema.GroupVersionKind{Version: "v1", Kind: "persistentvolumes"}

	if err := ReplacePluralWithKind(discovery, gvk); err != nil {
		t.Fatalf("unexpected discovery error: %v", err)
	}
	if gvk.Kind != "PersistentVolume" {
		t.Fatalf("kind=%q, want PersistentVolume", gvk.Kind)
	}
}
