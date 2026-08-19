// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package options

import (
	"slices"
	"testing"

	"k8s.io/client-go/rest"
)

func TestNewKubePreservesIgnoredIdentities(t *testing.T) {
	t.Parallel()

	ignoredGroups := []string{"platform", "operations"}
	ignoredUsernames := []string{"alice", "service-user"}

	opts, err := NewKube(
		nil,
		ignoredGroups,
		ignoredUsernames,
		"preferred_username",
		&rest.Config{Host: "https://kubernetes.example"},
		nil,
		"",
		false,
		nil,
		"X-Forwarded-Client-Cert",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	if got := opts.IgnoredGroupNames(); !slices.Equal(got, ignoredGroups) {
		t.Fatalf("IgnoredGroupNames() = %v, want %v", got, ignoredGroups)
	}
	if got := opts.IgnoredUsernames(); !slices.Equal(got, ignoredUsernames) {
		t.Fatalf("IgnoredUsernames() = %v, want %v", got, ignoredUsernames)
	}
}
