// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package clusterscoped

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

func parseSelectors(t testing.TB, expressions ...string) []labels.Selector {
	t.Helper()
	selectors := make([]labels.Selector, len(expressions))
	for i, expression := range expressions {
		selector, err := labels.Parse(expression)
		if err != nil {
			t.Fatal(err)
		}
		selectors[i] = selector
	}
	return selectors
}

func TestUnionSelectorsPreservesRuleBoundaries(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name        string
		expressions []string
		wantError   bool
	}{
		{"no rules", nil, false},
		{"single conjunction", []string{"tenant=a,shared=true"}, false},
		{"duplicate conjunction", []string{"tenant=a,shared=true", "shared=true,tenant=a"}, false},
		{"single varying key", []string{"tenant=a,shared=true", "tenant=b,shared=true", "tenant=c,shared=true"}, false},
		{"set values", []string{"tenant in (a,b),shared=true", "tenant in (b,c),shared=true"}, false},
		{"equal and in", []string{"tenant=a,shared=true", "tenant in (b,c),shared=true"}, false},
		{"common negative condition", []string{"tenant=a,environment notin (prod)", "tenant=b,environment notin (prod)"}, false},
		{"negative single rule", []string{"tenant notin (a),!shared"}, false},
		{"cross combinations", []string{"tenant=a,environment=dev", "tenant=b,environment=prod"}, true},
		{"different keys", []string{"tenant=a", "shared=true"}, true},
		{"different conjunction lengths", []string{"tenant=a", "tenant=b,shared=true"}, true},
		{"negative disjunction", []string{"tenant notin (a)", "tenant notin (b)"}, true},
		{"different operators", []string{"tenant=a", "tenant notin (b)"}, true},
		{"different presence", []string{"shared", "!shared"}, true},
		{"different varying dimensions", []string{"tenant=a,environment=dev", "tenant=b,environment=dev", "tenant=a,environment=prod"}, true},
		{"repeated key constraints", []string{"tenant in (a,b),tenant notin (b)", "tenant in (b,c),tenant notin (b)"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			selectors := parseSelectors(t, tt.expressions...)
			before := make([]string, len(selectors))
			for i, selector := range selectors {
				before[i] = selector.String()
			}
			combined, err := UnionSelectors(selectors)
			if (err != nil) != tt.wantError {
				t.Fatalf("selector=%v error=%v", combined, err)
			}
			for i, selector := range selectors {
				if selector.String() != before[i] {
					t.Fatal("input selector mutated")
				}
			}
			if tt.wantError || len(selectors) == 0 {
				if combined != nil {
					t.Fatalf("expected no selector, got %v", combined)
				}
				return
			}
			// Compare the forwarded selector to the original disjunction, including
			// absent labels and every cross-combination of the relevant values.
			for _, tenant := range []string{"", "a", "b", "c", "other"} {
				for _, shared := range []string{"", "true", "false"} {
					for _, environment := range []string{"", "dev", "prod"} {
						set := labels.Set{}
						for key, value := range map[string]string{"tenant": tenant, "shared": shared, "environment": environment} {
							if value != "" {
								set[key] = value
							}
						}
						want := slices.ContainsFunc(selectors, func(s labels.Selector) bool { return s.Matches(set) })
						if combined.Matches(set) != want {
							t.Fatalf("%v matches %v differently from original rules %v", combined, set, tt.expressions)
						}
					}
				}
			}
		})
	}
}

func TestGetRequiresCompleteSelector(t *testing.T) {
	t.Parallel()
	for _, resolved := range []bool{false, true} {
		for _, complete := range []bool{false, true} {
			t.Run(fmt.Sprintf("resolved=%t/complete=%t", resolved, complete), func(t *testing.T) {
				mod, discovery, reader, grants, request := namedResourceFixture(t, resolved, "selected")
				grants[0].ClusterResources[0].Selector.MatchLabels["tenant"] = "a"
				if complete {
					// A second complete rule can grant GET even when the first only partially matches.
					grants = append(grants, &tenant.ProxyTenant{ClusterResources: []v1beta1.ClusterResource{{APIGroups: []string{""}, Resources: []string{"persistentvolumes"}, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"selected": "true"}}}}})
				}
				selector, err := mod.Handle(grants, request)
				if err != nil || (selector != nil) != complete {
					t.Fatalf("selector=%v error=%v", selector, err)
				}
				if reader.calls.Load() != 1 || (resolved && discovery.calls.Load() != 0) {
					t.Fatal("selector matching added reads or discovery")
				}
			})
		}
	}
}

func BenchmarkClusterScopeSelectorPipeline(b *testing.B) {
	for _, tenants := range []int{1, 16} {
		for _, rules := range []int{1, 8, 64} {
			for _, unsafe := range []bool{false, true} {
				for _, parallel := range []bool{false, true} {
					b.Run(fmt.Sprintf("tenants=%d/rules=%d/unsafe=%t/parallel=%t", tenants, rules, unsafe, parallel), func(b *testing.B) {
						grants := make([]*tenant.ProxyTenant, tenants)
						for i := range grants {
							grants[i] = &tenant.ProxyTenant{}
							for j := range rules {
								shared := "true"
								if unsafe && j > 0 {
									shared = "false"
								}
								grants[i].ClusterResources = append(grants[i].ClusterResources, v1beta1.ClusterResource{APIGroups: []string{""}, Resources: []string{"namespaces"}, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": fmt.Sprintf("tenant-%d", j), "shared": shared}}})
							}
						}
						mod := List(nil, nil, "/api/v1/namespaces")
						request := staticRequest{Request: httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", nil)}
						run := func() {
							selector, err := mod.Handle(grants, request)
							if (err != nil) != (unsafe && rules > 1) || (selector == nil) != (err != nil) {
								b.Fatalf("selector=%v error=%v", selector, err)
							}
						}
						b.ReportAllocs()
						b.ResetTimer()
						if parallel {
							b.RunParallel(func(pb *testing.PB) {
								for pb.Next() {
									run()
								}
							})
						} else {
							for range b.N {
								run()
							}
						}
					})
				}
			}
		}
	}
}
