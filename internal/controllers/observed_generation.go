// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// reconcileObservedGeneration is a shared helper for controllers that only need to
// synchronise status.observedGeneration with metadata.generation.
func reconcileObservedGeneration[T client.Object](
	ctx context.Context,
	c client.Client,
	req reconcile.Request,
	newObj func() T,
	getObservedGeneration func(T) int64,
	setObservedGeneration func(T, int64),
) (reconcile.Result, error) {
	return reconcile.Result{}, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := newObj()
		if err := c.Get(ctx, req.NamespacedName, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		if getObservedGeneration(latest) == latest.GetGeneration() {
			return nil
		}

		original, ok := latest.DeepCopyObject().(client.Object)
		if !ok {
			return fmt.Errorf("DeepCopyObject returned unexpected type %T", latest)
		}

		setObservedGeneration(latest, latest.GetGeneration())

		if err := c.Status().Patch(ctx, latest, client.MergeFrom(original)); err != nil {
			if apierrors.IsNotFound(err) {
				// Re-GET to distinguish a deleted object from a missing status
				// subresource: the /status endpoint also returns 404 when the
				// subresource is not enabled, which would silently mask misconfiguration.
				check := newObj()
				if getErr := c.Get(ctx, req.NamespacedName, check); apierrors.IsNotFound(getErr) {
					return nil
				}

				// Object exists but the status patch returned 404: the CRD is
				// almost certainly missing `subresources: status` in its spec.
				return fmt.Errorf(
					"status patch for %s returned 404 but the object still exists: "+
						"ensure the CRD has subresources.status enabled: %w",
					req.NamespacedName, err,
				)
			}

			return err
		}

		return nil
	})
}
