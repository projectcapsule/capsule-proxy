// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ClusterResourceOperation is an operation capsule-proxy can perform on a
// selected cluster-scoped resource.
// +kubebuilder:validation:Enum=List;Get
type ClusterResourceOperation string

func (p ClusterResourceOperation) String() string {
	return string(p)
}

const (
	ClusterResourceOperationList ClusterResourceOperation = "List"
	ClusterResourceOperationGet  ClusterResourceOperation = "Get"
)

// AllowsOperation reports whether this rule enables the requested operation.
// An omitted operations field enables GET and LIST. LIST also implies GET to
// keep existing v1beta1 rules backward-compatible: LIST was historically the
// only supported value while named GET requests were always intercepted.
func (r ClusterResource) AllowsOperation(operation ClusterResourceOperation) bool {
	if len(r.Operations) == 0 {
		return operation == ClusterResourceOperationList || operation == ClusterResourceOperationGet
	}

	for _, configured := range r.Operations {
		if configured == operation ||
			(operation == ClusterResourceOperationGet && configured == ClusterResourceOperationList) {
			return true
		}
	}

	return false
}

// EffectiveOperations returns the normalized supported operations. Legacy
// LIST rules include GET; omitted operations default to both.
func (r ClusterResource) EffectiveOperations() []ClusterResourceOperation {
	list, get := len(r.Operations) == 0, len(r.Operations) == 0
	for _, operation := range r.Operations {
		switch operation {
		case ClusterResourceOperationList:
			list = true
			get = true
		case ClusterResourceOperationGet:
			get = true
		}
	}

	operations := make([]ClusterResourceOperation, 0, 2)
	if list {
		operations = append(operations, ClusterResourceOperationList)
	}

	if get {
		operations = append(operations, ClusterResourceOperationGet)
	}

	return operations
}

// ClusterResource Specification
// +kubebuilder:object:generate=true
type ClusterResource struct {
	// APIGroups is the name of the APIGroup that contains the resources. If multiple API groups are specified, any action requested against any resource listed will be allowed. '*' represents all resources. Empty string represents v1 api resources.
	APIGroups []string `json:"apiGroups"`

	// Resources is a list of resources this rule applies to. '*' represents all resources.
	Resources []string `json:"resources"`

	// Operations which can be executed on the selected resources. Only GET and
	// LIST are supported. When omitted, both operations are enabled. LIST also
	// enables GET for backward compatibility with existing v1beta1 rules.
	// +kubebuilder:default={List,Get}
	Operations []ClusterResourceOperation `json:"operations,omitempty"`

	// Select all cluster scoped resources with the given label selector.
	// Defining a selector which does not match any resources is considered not selectable (eg. using operation NotExists).
	Selector *metav1.LabelSelector `json:"selector"`
}
