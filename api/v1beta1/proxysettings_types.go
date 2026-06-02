// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type OwnerSpec struct {
	// Kind of tenant owner. Possible values are "User", "Group", and "ServiceAccount"
	Kind capsulerbac.OwnerKind `json:"kind"`
	// Name of tenant owner.
	Name string `json:"name"`
	// Cluster Resources for tenant Owner.
	ClusterResources []ClusterResource `json:"clusterResources,omitempty"`
	// Deprecated: Use Global Proxy Settings instead (https://projectcapsule.dev/docs/proxy/proxysettings/#globalproxysettings)
	//
	// Proxy settings for tenant owner.
	ProxyOperations []capsulerbac.ProxySettings `json:"proxySettings,omitempty"`
}

// ProxySettingSpec defines the additional Capsule Proxy settings for additional users of the Tenant.
// Resource is Namespace-scoped and applies the settings to the belonged Tenant.
type ProxySettingSpec struct {
	// Subjects that should receive additional permissions.
	// +kubebuilder:validation:MinItems=1
	Subjects []OwnerSpec `json:"subjects"`
}

// ProxySettingStatus defines the observed state of ProxySetting.
type ProxySettingStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions contains the reconciliation conditions for this ProxySetting.
	// +optional
	Conditions capmeta.ConditionList `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="Reconcile status of this ProxySetting"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

// ProxySetting is the Schema for the proxysettings API.
type ProxySetting struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ProxySettingSpec `json:"spec,omitempty"`
	// +optional
	Status ProxySettingStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ProxySettingList contains a list of ProxySetting.
type ProxySettingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ProxySetting `json:"items"`
}

//nolint:gochecknoinits
func init() {
	SchemeBuilder.Register(&ProxySetting{}, &ProxySettingList{})
}
