// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	"github.com/clastix/capsule/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type OwnerSpec struct {
	// Kind of tenant owner. Possible values are "User", "Group", and "ServiceAccount"
	Kind v1beta2.OwnerKind `json:"kind"`
	// Name of tenant owner.
	Name string `json:"name"`
	// Proxy settings for tenant owner.
	ProxyOperations []v1beta2.ProxySettings `json:"proxySettings,omitempty"`
}

// ProxySettingSpec defines the additional Capsule Proxy settings for additional users of the Tenant.
// Resource is Namespace-scoped and applies the settings to the belonged Tenant.
type ProxySettingSpec struct {
	// Subjects that should receive additional permissions.
	// +kubebuilder:validation:MinItems=1
	Subjects []OwnerSpec `json:"subjects"`
}

//+kubebuilder:object:root=true

// ProxySetting is the Schema for the proxysettings API.
type ProxySetting struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ProxySettingSpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// ProxySettingList contains a list of ProxySetting.
type ProxySettingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProxySetting `json:"items"`
}

//nolint:gochecknoinits
func init() {
	SchemeBuilder.Register(&ProxySetting{}, &ProxySettingList{})
}
