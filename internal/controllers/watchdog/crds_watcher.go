// Copyright 2020-2025 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package watchdog

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type resourceManager struct {
	cancelFn        context.CancelFunc
	watchedVersions sets.Set[string]
}

type watchMap map[string]resourceManager

type CRDWatcher struct {
	Client   client.Client
	watchMap watchMap
	requeue  chan event.GenericEvent
}

func (c *CRDWatcher) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	crd := apiextensionsv1.CustomResourceDefinition{}
	if err := c.Client.Get(ctx, request.NamespacedName, &crd); err != nil {
		if k8serrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	key := c.keyFunction(crd.Spec.Group, crd.Spec.Names.Kind)

	resourceMgr, found := c.watchMap[key]
	if !found && crd.DeletionTimestamp != nil {
		return reconcile.Result{}, nil
	}

	if !found && crd.DeletionTimestamp == nil {
		versions := make([]string, 0, len(crd.Spec.Versions))

		for _, v := range crd.Spec.Versions {
			versions = append(versions, v.Name)
		}

		if err := c.register(ctx, crd.Spec.Group, versions, crd.Spec.Names.Kind); err != nil {
			return reconcile.Result{}, err
		}

		resourceMgr = c.watchMap[key]
	}

	if crd.DeletionTimestamp != nil {
		resourceMgr.cancelFn()
		delete(c.watchMap, key)

		return reconcile.Result{}, nil
	}

	for _, v := range crd.Spec.Versions {
		if !resourceMgr.watchedVersions.Has(v.Name) {
			resourceMgr.cancelFn()
			delete(c.watchMap, key)

			return reconcile.Result{Requeue: true}, nil
		}
	}

	return reconcile.Result{}, nil
}

func (c *CRDWatcher) SetupWithManager(ctx context.Context, mgr manager.Manager) error {
	c.watchMap = make(map[string]resourceManager)
	c.requeue = make(chan event.GenericEvent)

	apis, err := API(mgr.GetConfig())
	if err != nil {
		return err
	}

	bundleGroupAndKind := map[string]sets.Set[string]{}

	for _, api := range apis {
		slashedName := fmt.Sprintf("%s/%s", api.Group, api.Kind)

		if _, ok := bundleGroupAndKind[slashedName]; !ok {
			bundleGroupAndKind[slashedName] = sets.Set[string]{}
		}

		bundleGroupAndKind[slashedName].Insert(api.Version)
	}

	for group, versions := range bundleGroupAndKind {
		parts := strings.Split(group, "/")

		apiGroup, apiKind := parts[0], parts[1]

		if registerErr := c.register(ctx, apiGroup, versions.UnsortedList(), apiKind); registerErr != nil {
			return errors.Wrap(registerErr, "cannot register watcher prior to start-up")
		}
	}

	return ctrl.NewControllerManagedBy(mgr).
		WatchesRawSource(source.Channel(c.requeue, &handler.EnqueueRequestForObject{})).
		For(&apiextensionsv1.CustomResourceDefinition{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
			//nolint:forcetypeassert
			crd := object.(*apiextensionsv1.CustomResourceDefinition)

			return crd.Spec.Scope == apiextensionsv1.NamespaceScoped
		}))).
		Complete(c)
}

func (c *CRDWatcher) keyFunction(group, kind string) string {
	return fmt.Sprintf("%s-%s", group, kind)
}

func (c *CRDWatcher) register(ctx context.Context, group string, versions []string, kind string) error {
	mgr, _ := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: c.Client.Scheme(),
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})

	watchedVersions := sets.New[string]()

	for _, v := range versions {
		watchedVersions.Insert(v)

		gvk := metav1.GroupVersionKind{
			Group:   group,
			Version: v,
			Kind:    kind,
		}
		//nolint:contextcheck
		if err := (&NamespacedWatcher{Client: c.Client}).SetupWithManager(mgr, gvk); err != nil {
			return err
		}
	}

	scopedCtx, scopedCancelFn := context.WithCancel(ctx)

	go func() {
		if err := mgr.Start(scopedCtx); err != nil {
			scopedCancelFn()
		}
	}()

	c.watchMap[c.keyFunction(group, kind)] = resourceManager{
		cancelFn:        scopedCancelFn,
		watchedVersions: watchedVersions,
	}

	return nil
}
