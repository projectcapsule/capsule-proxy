// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"testing"

	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
)

func TestNewClusterProxySubjects(t *testing.T) {
	t.Parallel()

	rule := func(resource string, subjects ...v1beta1.GlobalSubject) v1beta1.GlobalSubjectSpec {
		return v1beta1.GlobalSubjectSpec{
			Subjects:         subjects,
			ClusterResources: []v1beta1.ClusterResource{{APIGroups: []string{""}, Resources: []string{resource}}},
		}
	}

	rules := []v1beta1.GlobalSubjectSpec{
		rule("namespaces", v1beta1.GlobalSubject{Kind: v1beta1.GlobalSubjectKindUser, Name: "alice"}),
		rule("nodes", v1beta1.GlobalSubject{Kind: v1beta1.GlobalSubjectKindTenant, Name: "solar"}),
		// Matching through both subjects must not duplicate the rule.
		rule("pods",
			v1beta1.GlobalSubject{Kind: v1beta1.GlobalSubjectKindTenant, Name: "solar"},
			v1beta1.GlobalSubject{Kind: v1beta1.GlobalSubjectKindUser, Name: "alice"},
		),
		// A Tenant subject never matches an identity by name.
		rule("secrets", v1beta1.GlobalSubject{Kind: v1beta1.GlobalSubjectKindTenant, Name: "alice"}),
	}

	tests := []struct {
		name         string
		ownerName    string
		ownerKind    capsulerbac.OwnerKind
		ownedTenants []string
		want         []string
	}{
		{"direct subject only", "alice", capsulerbac.UserOwner, nil, []string{"namespaces", "pods"}},
		{"owner of the Tenant", "bob", capsulerbac.UserOwner, []string{"solar"}, []string{"nodes", "pods"}},
		{"direct subject and Tenant owner", "alice", capsulerbac.UserOwner, []string{"solar"}, []string{"namespaces", "nodes", "pods"}},
		{"owner of another Tenant", "bob", capsulerbac.UserOwner, []string{"wind"}, nil},
		{"identity named like a Tenant subject", "alice", capsulerbac.GroupOwner, nil, nil},
		{"identity named like a Tenant", "alice", capsulerbac.UserOwner, []string{"lunar"}, []string{"namespaces", "pods"}},
		{"Tenant name equal to subject name", "bob", capsulerbac.UserOwner, []string{"alice"}, []string{"secrets"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pt := NewClusterProxy(tc.ownerName, tc.ownerKind, rules, tc.ownedTenants)

			var got []string
			for _, resource := range pt.ClusterResources {
				got = append(got, resource.Resources...)
			}

			if len(got) != len(tc.want) {
				t.Fatalf("got resources %v, want %v", got, tc.want)
			}

			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got resources %v, want %v", got, tc.want)
				}
			}
		})
	}
}
