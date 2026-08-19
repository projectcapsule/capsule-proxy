// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsuleproxyv1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
)

func TestValidateClusterResourceOperationsAndDiscoveryVerbs(t *testing.T) {
	t.Parallel()

	index := &ClusterResourceDiscoveryIndex{byGroup: map[string]map[string]DiscoveredClusterResource{
		"storage.k8s.io": {
			"both":      {Kinds: []string{"Both"}, Verbs: []string{"get", "list"}},
			"get-only":  {Kinds: []string{"GetOnly"}, Verbs: []string{"get"}},
			"list-only": {Kinds: []string{"ListOnly"}, Verbs: []string{"list"}},
		},
	}}

	tests := []struct {
		name       string
		resource   string
		operations []capsuleproxyv1beta1.ClusterResourceOperation
		wantError  string
	}{
		{name: "omitted operations default to get and list", resource: "both"},
		{name: "explicit get", resource: "get-only", operations: []capsuleproxyv1beta1.ClusterResourceOperation{capsuleproxyv1beta1.ClusterResourceOperationGet}},
		{name: "explicit list includes get", resource: "both", operations: []capsuleproxyv1beta1.ClusterResourceOperation{capsuleproxyv1beta1.ClusterResourceOperationList}},
		{name: "legacy list requires get", resource: "list-only", operations: []capsuleproxyv1beta1.ClusterResourceOperation{capsuleproxyv1beta1.ClusterResourceOperationList}, wantError: "does not support Get"},
		{name: "default requires get", resource: "list-only", wantError: "does not support Get"},
		{name: "default requires list", resource: "get-only", wantError: "does not support List"},
		{name: "update is rejected", resource: "both", operations: []capsuleproxyv1beta1.ClusterResourceOperation{"Update"}, wantError: "unsupported operation"},
		{name: "delete is rejected", resource: "both", operations: []capsuleproxyv1beta1.ClusterResourceOperation{"Delete"}, wantError: "unsupported operation"},
		{name: "operation wildcard is rejected", resource: "both", operations: []capsuleproxyv1beta1.ClusterResourceOperation{"*"}, wantError: "unsupported operation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateClusterResourceBlock("spec.rules[0].clusterResources[0]", capsuleproxyv1beta1.ClusterResource{
				APIGroups:  []string{"storage.k8s.io"},
				Resources:  []string{tt.resource},
				Operations: tt.operations,
				Selector:   &metav1.LabelSelector{MatchLabels: map[string]string{"allowed": "true"}},
			}, index)

			switch {
			case tt.wantError == "" && err != nil:
				t.Fatalf("unexpected validation error: %v", err)
			case tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError)):
				t.Fatalf("expected error containing %q, got %v", tt.wantError, err)
			}
		})
	}
}
