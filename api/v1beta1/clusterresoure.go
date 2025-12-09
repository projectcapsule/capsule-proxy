// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +kubebuilder:validation:Enum=List;Update;Delete
type ClusterResourceOperation string

func (p ClusterResourceOperation) String() string {
	return string(p)
}

const (
	ClusterResourceOperationList ClusterResourceOperation = "List"
)

// +kubebuilder:object:generate=true
type ClusterResource struct {
	// APIGroups is the name of the APIGroup that contains the resources. If multiple API groups are specified, any action requested against any resource listed will be allowed. '*' represents all resources. Empty string represents v1 api resources.
	APIGroups []string `json:"apiGroups"`

	// Resources is a list of resources this rule applies to. '*' represents all resources.
	Resources []string `json:"resources"`

	// Operations which can be executed on the selected resources.
	// Deprecated: For all registered Routes only LIST ang GET requests will intercepted
	// Other permissions must be implemented via kubernetes native RBAC
	Operations []ClusterResourceOperation `json:"operations,omitempty"`

	// Select all cluster scoped resources with the given label selector.
	// Defining a selector which does not match any resources is considered not selectable (eg. using operation NotExists).
	Selector *metav1.LabelSelector `json:"selector"`
}
