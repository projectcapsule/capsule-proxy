// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type capsuleStartupReader struct {
	client.Reader
	failure error
}

func (r capsuleStartupReader) Get(ctx context.Context, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
	if r.failure != nil {
		return r.failure
	}
	return r.Reader.Get(ctx, key, object, opts...)
}

func TestCLICapsuleAvailability(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&capsulev1beta2.CapsuleConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "configured"}}).Build()
	for _, require := range []bool{true, false} {
		for _, tc := range []struct {
			name    string
			failure error
			found   bool
		}{
			{name: "configured", found: true},
			{name: "missing"},
			{name: "absent-api", failure: &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "capsule.clastix.io", Kind: "CapsuleConfiguration"}}},
			{name: "forbidden", failure: apierrors.NewForbidden(schema.GroupResource{Group: "capsule.clastix.io", Resource: "capsuleconfigurations"}, "configured", errors.New("denied"))},
			{name: "unavailable", failure: errors.New("apiserver unavailable")},
		} {
			t.Run(tc.name+"/require="+map[bool]string{true: "true", false: "false"}[require], func(t *testing.T) {
				c := parseCLI(t, "--capsule-configuration-name="+tc.name, "--enable-reflector=true")
				c.requireCapsuleUser = require
				err := c.detectCapsule(t.Context(), capsuleStartupReader{Reader: reader, failure: tc.failure})
				optionalMissing := !require && (tc.name == "missing" || tc.name == "absent-api")
				if (err == nil) != (tc.found || optionalMissing) {
					t.Fatalf("detectCapsule error=%v", err)
				}
				if err == nil && cliListener(t, c).CapsuleAvailable() != tc.found {
					t.Fatalf("CapsuleAvailable=%t want=%t", c.capsuleAvailable, tc.found)
				}
				if err == nil && c.reflectorEnabled() != tc.found {
					t.Fatal("standalone startup must disable tenant-dependent reflection")
				}
			})
		}
	}
	if !cliListener(t, parseCLI(t)).RequireCapsuleUser() {
		t.Fatal("default must require Capsule identities")
	}
	if cliListener(t, parseCLI(t, "--require-capsule-user=true", "--require-capsule-user=false")).RequireCapsuleUser() {
		t.Fatal("last flag value must win")
	}
	if !cliListener(t, parseCLI(t)).RequireCapsuleUser() {
		t.Fatal("repeated parses leaked flag state")
	}
}
