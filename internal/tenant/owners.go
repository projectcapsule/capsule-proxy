// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/indexer"
)

type ownerTenantsKey struct{}

type ownerTenants struct {
	kind    capsulerbac.OwnerKind
	name    string
	tenants *capsulev1beta2.TenantList
}

// WithOwnerTenants carries an immutable ownership snapshot through one request.
// It does not retain authentication or authorization decisions across requests.
func WithOwnerTenants(ctx context.Context, kind capsulerbac.OwnerKind, name string, tenants *capsulev1beta2.TenantList) context.Context {
	return context.WithValue(ctx, ownerTenantsKey{}, ownerTenants{kind: kind, name: name, tenants: tenants})
}

// TenantsForOwner uses the manager's owner index, reusing an identity-matched
// snapshot when middleware has already resolved ownership for this request.
func TenantsForOwner(ctx context.Context, reader client.Reader, kind capsulerbac.OwnerKind, name string) (*capsulev1beta2.TenantList, error) {
	if snapshot, ok := ctx.Value(ownerTenantsKey{}).(ownerTenants); ok && snapshot.kind == kind && snapshot.name == name && snapshot.tenants != nil {
		return snapshot.tenants, nil
	}

	tenants := &capsulev1beta2.TenantList{}
	if err := reader.List(ctx, tenants, client.MatchingFields{indexer.TenantOwnerKindField: fmt.Sprintf("%s:%s", kind.String(), name)}); err != nil {
		return nil, err
	}

	return tenants, nil
}
