// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
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

// GlobalSubjectKind is the kind of a GlobalProxySettings subject.
// +kubebuilder:validation:Enum=User;Group;ServiceAccount;Tenant
type GlobalSubjectKind string

const (
	GlobalSubjectKindUser           GlobalSubjectKind = GlobalSubjectKind(capsulerbac.UserOwner)
	GlobalSubjectKindGroup          GlobalSubjectKind = GlobalSubjectKind(capsulerbac.GroupOwner)
	GlobalSubjectKindServiceAccount GlobalSubjectKind = GlobalSubjectKind(capsulerbac.ServiceAccountOwner)
	// GlobalSubjectKindTenant selects every current owner of the Tenant with the given name.
	// Owners are resolved from the Tenant status, so they can be Users, Groups or promoted ServiceAccounts.
	GlobalSubjectKindTenant GlobalSubjectKind = "Tenant"
)

func (k GlobalSubjectKind) String() string {
	return string(k)
}

type GlobalSubject struct {
	// Kind of the subject. Possible values are "User", "Group", "ServiceAccount" and "Tenant".
	// A "Tenant" subject matches every current owner of the named Tenant.
	Kind GlobalSubjectKind `json:"kind"`
	// Name of the subject. For the "Tenant" kind, the name of the Tenant.
	Name string `json:"name"`
}

// GlobalProxySettingsStatus defines the observed state of GlobalProxySettings.
type GlobalProxySettingsStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions contains the reconciliation conditions for this GlobalProxySettings.
	// +optional
	Conditions capmeta.ConditionList `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="Reconcile status of this GlobalProxySettings"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

// GlobalProxySettings is the Schema for the globalproxysettings API.
type GlobalProxySettings struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec GlobalProxySettingsSpec `json:"spec,omitempty"`
	// +optional
	Status GlobalProxySettingsStatus `json:"status,omitempty"`
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
