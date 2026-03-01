// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package indexer

import (
	"fmt"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	TenantOwnerKindField = ".status.owner.ownerkind"
)

// TenantOwnerReference indexes Tenants by their status.owners (Kind:Name).
type TenantOwnerReference struct{}

func (o TenantOwnerReference) Object() client.Object {
	return &capsulev1beta2.Tenant{}
}

func (o TenantOwnerReference) Field() string {
	return TenantOwnerKindField
}

func (o TenantOwnerReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		tnt, ok := object.(*capsulev1beta2.Tenant)
		if !ok {
			panic(fmt.Errorf("expected type *capsulev1beta2.Tenant, got %T", object))
		}

		var owners []string
		for _, owner := range tnt.Status.Owners {
			owners = append(owners, fmt.Sprintf("%s:%s", owner.Kind.String(), owner.Name))
		}

		return owners
	}
}
