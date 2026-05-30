// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package controllers //nolint:dupl // ProxySettingReconciler and GlobalProxySettingsReconciler are intentionally parallel thin wrappers for different CRD types; shared logic lives in observed_generation.go.

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsuleproxyv1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
)

// ProxySettingReconciler updates status.observedGeneration after each reconcile.
type ProxySettingReconciler struct {
	Client client.Client
}

func (r *ProxySettingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsuleproxyv1beta1.ProxySetting{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

func (r *ProxySettingReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	return reconcileObservedGeneration(ctx, r.Client, req,
		func() *capsuleproxyv1beta1.ProxySetting { return &capsuleproxyv1beta1.ProxySetting{} },
		func(o *capsuleproxyv1beta1.ProxySetting) int64 { return o.Status.ObservedGeneration },
		func(o *capsuleproxyv1beta1.ProxySetting, gen int64) { o.Status.ObservedGeneration = gen },
	)
}
