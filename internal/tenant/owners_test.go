// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/projectcapsule/capsule-proxy/internal/indexer"
)

func TestOwnerTenantsRequestSnapshotScope(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	a := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"}, Status: capsulev1beta2.TenantStatus{Owners: capsulerbac.OwnerStatusListSpec{{UserSpec: capsulerbac.UserSpec{Kind: capsulerbac.ServiceAccountOwner, Name: "system:serviceaccount:a:reader"}}}}}
	b := a.DeepCopy()
	b.Name = "tenant-b"
	b.Status.Owners[0].Name = "system:serviceaccount:b:reader"
	idx := indexer.TenantOwnerReference{}
	var calls int
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a, b).WithIndex(idx.Object(), idx.Field(), idx.Func()).WithInterceptorFuncs(interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, obj client.ObjectList, opts ...client.ListOption) error {
		calls++
		return c.List(ctx, obj, opts...)
	}}).Build()
	first, err := TenantsForOwner(t.Context(), c, capsulerbac.ServiceAccountOwner, a.Status.Owners[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.Items[0].Name != "tenant-a" {
		t.Fatal("owner index selected another tenant")
	}
	ctx := WithOwnerTenants(t.Context(), capsulerbac.ServiceAccountOwner, a.Status.Owners[0].Name, first)
	reused, err := TenantsForOwner(ctx, c, capsulerbac.ServiceAccountOwner, a.Status.Owners[0].Name)
	if err != nil || reused != first || calls != 1 {
		t.Fatalf("snapshot not reused: %v calls=%d", err, calls)
	}
	for _, candidate := range []struct {
		kind   capsulerbac.OwnerKind
		name   string
		tenant string
	}{{capsulerbac.ServiceAccountOwner, b.Status.Owners[0].Name, "tenant-b"}, {capsulerbac.UserOwner, a.Status.Owners[0].Name, ""}, {capsulerbac.ServiceAccountOwner, "system:serviceaccount:a:other", ""}} {
		t.Run(fmt.Sprintf("%s/%s", candidate.kind, candidate.name), func(t *testing.T) {
			previous := calls
			list, err := TenantsForOwner(ctx, c, candidate.kind, candidate.name)
			if err != nil {
				t.Fatal(err)
			}
			if calls != previous+1 {
				t.Fatal("snapshot leaked across an identity or subject kind")
			}
			if candidate.tenant == "" {
				if len(list.Items) != 0 {
					t.Fatal("unexpected owner")
				}
			} else if len(list.Items) != 1 || list.Items[0].Name != candidate.tenant {
				t.Fatal("wrong tenant")
			}
		})
	}
	nilCtx := WithOwnerTenants(t.Context(), capsulerbac.ServiceAccountOwner, a.Status.Owners[0].Name, nil)
	previous := calls
	if _, err := TenantsForOwner(nilCtx, c, capsulerbac.ServiceAccountOwner, a.Status.Owners[0].Name); err != nil {
		t.Fatal(err)
	}
	if calls != previous+1 {
		t.Fatal("nil snapshot must not bypass ownership lookup")
	}
}
