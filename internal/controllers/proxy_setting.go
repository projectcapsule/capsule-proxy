// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"

	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsuleproxyv1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
)

// ProxySettingReconciler reconciles ProxySetting objects and keeps
// status.observedGeneration in sync with metadata.generation.
type ProxySettingReconciler struct {
	Client client.Client
	reader client.Reader
}

func (r *ProxySettingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.reader = mgr.GetAPIReader()

	return ctrl.NewControllerManagedBy(mgr).
		For(&capsuleproxyv1beta1.ProxySetting{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

func (r *ProxySettingReconciler) Reconcile(ctx context.Context, req reconcile.Request) (result reconcile.Result, err error) { //nolint:dupl
	instance := &capsuleproxyv1beta1.ProxySetting{}
	if err = r.Client.Get(ctx, req.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	defer func() {
		if uerr := r.updateStatus(ctx, instance); uerr != nil {
			err = fmt.Errorf("cannot update ProxySetting status: %w", uerr)
		}
	}()

	return reconcile.Result{}, nil
}

func (r *ProxySettingReconciler) updateStatus(ctx context.Context, instance *capsuleproxyv1beta1.ProxySetting) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &capsuleproxyv1beta1.ProxySetting{}
		if err := r.reader.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		latest.Status.ObservedGeneration = latest.GetGeneration()

		readyCondition := capmeta.NewReadyCondition(latest)
		readyCondition.ObservedGeneration = latest.GetGeneration()
		latest.Status.Conditions.UpdateConditionByType(readyCondition)

		if err := r.Client.Status().Update(ctx, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		return nil
	})
}
