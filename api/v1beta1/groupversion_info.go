// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

// Package v1beta1 contains API Schema definitions for the capsule.clastix.io v1beta1 API group
// +kubebuilder:object:generate=true
// +groupName=capsule.clastix.io
package v1beta1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is group version used to register these objects.
	//nolint:gochecknoglobals
	GroupVersion = schema.GroupVersion{Group: "capsule.clastix.io", Version: "v1beta1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	//nolint:gochecknoglobals
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	//nolint:gochecknoglobals
	AddToScheme = SchemeBuilder.AddToScheme
)
