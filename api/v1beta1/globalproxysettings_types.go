// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	"github.com/projectcapsule/capsule/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GlobalProxySettingsSpec defines the desired state of GlobalProxySettings.
type GlobalProxySettingsSpec struct {
	// Subjects that should receive additional permissions.
	// The subjects are selected based on the oncoming requests. They don't have to relate to an existing tenant.
	// However they must be part of the capsule-user groups.
	// +kubebuilder:validation:MinItems=1
	Rules []GlobalSubjectSpec `json:"rules"`
}

type GlobalSubjectSpec struct {
	// Subjects that should receive additional permissions.
	// The subjects are selected based on the oncoming requests. They don't have to relate to an existing tenant.
	// However they must be part of the capsule-user groups.
	Subjects []GlobalSubject `json:"subjects"`
	// Cluster Resources for tenant Owner.
	ClusterResources []ClusterResource `json:"clusterResources,omitempty"`
}

type GlobalSubject struct {
	// Kind of tenant owner. Possible values are "User", "Group", and "ServiceAccount".
	Kind v1beta2.OwnerKind `json:"kind"`
	// Name of tenant owner.
	Name string `json:"name"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// GlobalProxySettings is the Schema for the globalproxysettings API.
type GlobalProxySettings struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec GlobalProxySettingsSpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// GlobalProxySettingsList contains a list of GlobalProxySettings.
type GlobalProxySettingsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []GlobalProxySettings `json:"items"`
}

//nolint:gochecknoinits
func init() {
	SchemeBuilder.Register(&GlobalProxySettings{}, &GlobalProxySettingsList{})
}
