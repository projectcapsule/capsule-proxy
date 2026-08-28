// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package options

import (
	"slices"
	"strings"
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

func TestNewKubePreservesPathOptions(t *testing.T) {
	t.Parallel()

	allowedPaths := []string{"/api", "/apis", "/version"}
	publicPaths := []string{"/public"}

	opts, err := NewKube(
		nil,
		nil,
		nil,
		"preferred_username",
		&rest.Config{Host: "https://kubernetes.example"},
		nil,
		"",
		false,
		nil,
		"X-Forwarded-Client-Cert",
		allowedPaths,
		publicPaths,
	)
	if err != nil {
		t.Fatal(err)
	}

	if got := opts.AllowedPaths(); !slices.Equal(got, allowedPaths) {
		t.Fatalf("AllowedPaths() = %v, want %v", got, allowedPaths)
	}
	if got := opts.PublicPaths(); !slices.Equal(got, publicPaths) {
		t.Fatalf("PublicPaths() = %v, want %v", got, publicPaths)
	}
}

func TestNewKubeRejectsOverlappingPathOptions(t *testing.T) {
	t.Parallel()

	_, err := NewKube(
		nil,
		nil,
		nil,
		"preferred_username",
		&rest.Config{Host: "https://kubernetes.example"},
		nil,
		"",
		false,
		nil,
		"X-Forwarded-Client-Cert",
		[]string{"/api", "/version"},
		[]string{"/version"},
	)
	if err == nil {
		t.Fatal("NewKube() succeeded with overlapping allowed and public paths")
	}

	if !strings.Contains(err.Error(), "/version") {
		t.Fatalf("NewKube() error = %q, want overlapping path", err)
	}
}
