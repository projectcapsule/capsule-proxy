// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package clusterscoped

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"

	// adjust import to your actual api package
	v1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
)

func TestMatchPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		value   string
		want    bool
	}{
		// Wildcard trivial
		{"star_matches_any_empty", "*", "", true},
		{"star_matches_any_nonempty", "*", "anything", true},

		// Exact match (no wildcard)
		{"exact_equal", "apps", "apps", true},
		{"exact_not_equal", "apps", "core", false},
		{"exact_empty_matches_empty", "", "", true},
		{"exact_empty_not_match_nonempty", "", "x", false},

		// Prefix/suffix optimizations (single '*')
		{"prefix_star_suffix_match", "*v1", "apps/v1", true},
		{"prefix_star_suffix_no_match", "*v1", "apps/v2", false},
		{"suffix_star_prefix_match", "apps*", "apps/v1", true},
		{"suffix_star_prefix_no_match", "apps*", "batch/v1", false},

		// Middle wildcard (single '*', not prefix/suffix-only)
		{"middle_wildcard_match", "a*b", "acb", true},
		{"middle_wildcard_match_long", "a*b", "a---b", true},
		{"middle_wildcard_no_match_missing_suffix", "a*b", "a---c", false},
		{"middle_wildcard_no_match_missing_prefix", "a*b", "c---b", false},

		// Multiple wildcards
		{"multi_wildcards_match", "a*b*c", "aXXbYYc", true},
		{"multi_wildcards_match_adjacent", "a**c", "abc", true},
		{"multi_wildcards_match_with_empty_parts", "**", "anything", true},
		{"multi_wildcards_match_only_prefix", "**suffix", "prefixsuffix", true},
		{"multi_wildcards_match_only_suffix", "prefix**", "prefixsuffix", true},

		// Ordering constraints
		{"order_matters_true", "a*b*c", "a1b2c3", true},
		{"order_matters_false", "a*b*c", "a1c2b3", false},

		// Boundary cases
		{"value_empty_pattern_nonempty_with_star", "a*", "", false},
		{"pattern_star_only_parts", "*", "x", true},
		{"pattern_only_star_characters", "***", "x", true},
		{"pattern_only_star_characters_empty_value", "***", "", true},

		// Literal characters (no escaping semantics, '*' is the only special char)
		{"dot_is_literal", "a.b", "a.b", true},
		{"dot_is_literal_no_match", "a.b", "acb", false},
		{"slash_is_literal", "apps/v1", "apps/v1", true},
		{"slash_is_literal_no_match", "apps/v1", "apps/v2", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := matchPattern(tt.pattern, tt.value)
			if got != tt.want {
				t.Fatalf("matchPattern(%q,%q) = %v, want %v", tt.pattern, tt.value, got, tt.want)
			}
		})
	}
}

func TestMatchPattern_IdempotentLikeBehavior(t *testing.T) {
	t.Parallel()

	// Not truly "idempotent", but ensure determinism (same input -> same output)
	cases := []struct {
		pattern string
		value   string
	}{
		{"apps*", "apps/v1"},
		{"*v1", "apps/v1"},
		{"a*b*c", "a--b--c"},
		{"", ""},
		{"***", "x"},
	}

	for _, c := range cases {
		a := matchPattern(c.pattern, c.value)
		b := matchPattern(c.pattern, c.value)
		if a != b {
			t.Fatalf("matchPattern not deterministic for pattern=%q value=%q", c.pattern, c.value)
		}
	}
}

func TestMatchResource(t *testing.T) {
	t.Parallel()

	gvkCoreV1NS := &schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
	gvkAppsV1Deploy := &schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	gvkAppsV1SS := &schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"}
	gvkBatchV1Job := &schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "Job"}

	tests := []struct {
		name string
		gvk  *schema.GroupVersionKind
		cr   v1beta1.ClusterResource
		want bool
	}{
		// ---- Kind/resource matching ----
		{
			name: "resource_star_matches_any_kind_and_group_star",
			gvk:  gvkAppsV1Deploy,
			cr: v1beta1.ClusterResource{
				Resources: []string{"*"},
				APIGroups: []string{"*"},
			},
			want: true,
		},
		{
			name: "resource_exact_kind_matches_with_group_star",
			gvk:  gvkAppsV1Deploy,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Deployment"},
				APIGroups: []string{"*"},
			},
			want: true,
		},
		{
			name: "resource_kind_no_match_returns_false_even_if_group_matches",
			gvk:  gvkAppsV1Deploy,
			cr: v1beta1.ClusterResource{
				Resources: []string{"StatefulSet"},
				APIGroups: []string{"apps"},
			},
			want: false,
		},
		{
			name: "resource_list_any_one_match_is_enough",
			gvk:  gvkAppsV1SS,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Deployment", "StatefulSet"},
				APIGroups: []string{"apps"},
			},
			want: true,
		},
		{
			name: "resource_empty_list_never_matches",
			gvk:  gvkAppsV1Deploy,
			cr: v1beta1.ClusterResource{
				Resources: nil,
				APIGroups: []string{"*"},
			},
			want: false,
		},

		// ---- Group matching (RBAC-style: group only) ----
		{
			name: "apiGroup_star_matches_any_group_once_kind_matches",
			gvk:  gvkBatchV1Job,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Job"},
				APIGroups: []string{"*"},
			},
			want: true,
		},
		{
			name: "apiGroup_exact_matches_group",
			gvk:  gvkAppsV1Deploy,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Deployment"},
				APIGroups: []string{"apps"},
			},
			want: true,
		},
		{
			name: "apiGroup_exact_no_match_group",
			gvk:  gvkAppsV1Deploy,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Deployment"},
				APIGroups: []string{"batch"},
			},
			want: false,
		},
		{
			name: "apiGroup_core_empty_string_matches_core_group",
			gvk:  gvkCoreV1NS,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Namespace"},
				APIGroups: []string{""}, // core API group
			},
			want: true,
		},
		{
			name: "apiGroup_core_empty_string_does_not_match_non_core_group",
			gvk:  gvkAppsV1Deploy,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Deployment"},
				APIGroups: []string{""},
			},
			want: false,
		},
		{
			name: "apiGroups_any_one_match_is_enough",
			gvk:  gvkBatchV1Job,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Job"},
				APIGroups: []string{"apps", "batch"},
			},
			want: true,
		},
		{
			name: "apiGroups_empty_list_never_matches_even_if_kind_matches",
			gvk:  gvkAppsV1Deploy,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Deployment"},
				APIGroups: nil,
			},
			want: false,
		},

		// ---- Group/Version matching (extended: pattern contains '/') ----
		{
			name: "groupVersion_exact_matches",
			gvk:  gvkAppsV1Deploy,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Deployment"},
				APIGroups: []string{"apps/v1"},
			},
			want: true,
		},
		{
			name: "groupVersion_exact_no_match_wrong_version",
			gvk:  gvkAppsV1Deploy,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Deployment"},
				APIGroups: []string{"apps/v2"},
			},
			want: false,
		},
		{
			name: "groupVersion_wildcard_version_prefix",
			gvk:  gvkAppsV1Deploy,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Deployment"},
				APIGroups: []string{"apps/*"},
			},
			want: true,
		},
		{
			name: "groupVersion_wildcard_group_suffix",
			gvk:  gvkAppsV1Deploy,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Deployment"},
				APIGroups: []string{"*/v1"},
			},
			want: true,
		},
		{
			name: "groupVersion_wildcard_group_suffix_no_match",
			gvk:  &schema.GroupVersionKind{Group: "apps", Version: "v2", Kind: "Deployment"},
			cr: v1beta1.ClusterResource{
				Resources: []string{"Deployment"},
				APIGroups: []string{"*/v1"},
			},
			want: false,
		},
		{
			name: "groupVersion_middle_wildcard",
			gvk:  gvkAppsV1Deploy,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Deployment"},
				APIGroups: []string{"a*s/v1"},
			},
			want: true,
		},
		{
			name: "groupVersion_multiple_patterns_any_one_match",
			gvk:  gvkAppsV1Deploy,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Deployment"},
				APIGroups: []string{"batch/*", "apps/v1"},
			},
			want: true,
		},
		{
			name: "group_only_pattern_does_not_accidentally_match_group_version_target",
			gvk:  gvkAppsV1Deploy,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Deployment"},
				APIGroups: []string{"apps"}, // should match on group only (it does)
			},
			want: true,
		},

		// ---- Sanity: kind match required even when group matches ----
		{
			name: "group_matches_but_kind_does_not",
			gvk:  gvkAppsV1Deploy,
			cr: v1beta1.ClusterResource{
				Resources: []string{"Job"},
				APIGroups: []string{"apps"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := matchResource(tt.gvk, tt.cr)
			if got != tt.want {
				t.Fatalf("matchResource(gvk=%+v, cr=%+v) = %v, want %v", *tt.gvk, tt.cr, got, tt.want)
			}
		})
	}
}
