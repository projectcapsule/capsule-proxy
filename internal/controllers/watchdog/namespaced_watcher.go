package watchdog

import (
	"context"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	log2 "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulelabels "github.com/projectcapsule/capsule-proxy/internal/labels"
)

type NamespacedWatcher struct {
	Client         client.Client
	LeaderElection bool

	object *unstructured.Unstructured
}

func (c *NamespacedWatcher) NeedLeaderElection() bool {
	return c.LeaderElection
}

func (c *NamespacedWatcher) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := log2.FromContext(ctx)

	obj := c.object.DeepCopy()
	obj.SetName(request.Name)
	obj.SetNamespace(request.Namespace)

	tntList := capsulev1beta2.TenantList{}
	if err := c.Client.List(ctx, &tntList, client.MatchingFields{".status.namespaces": obj.GetNamespace()}); err != nil {
		log.Error(err, "cannot list unstructured object")

		return reconcile.Result{}, err
	}

	if len(tntList.Items) == 0 {
		return reconcile.Result{}, nil
	}

	if err := c.Client.Get(ctx, request.NamespacedName, obj); err != nil {
		if k8serrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}

		log.Error(err, "cannot retrieve object")

		return reconcile.Result{}, err
	}

	_, err := controllerutil.CreateOrUpdate(ctx, c.Client, obj, func() error {
		labels := obj.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}

		labels[capsulelabels.ManagedByCapsuleLabel] = tntList.Items[0].Name
		obj.SetLabels(labels)

		return nil
	})

	return reconcile.Result{}, err
}

func (c *NamespacedWatcher) SetupWithManager(mgr manager.Manager, gvk metav1.GroupVersionKind) error {
	obj := unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	})

	c.object = obj.DeepCopy()

	return controllerruntime.NewControllerManagedBy(mgr).
		For(&obj, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				ns := &corev1.Namespace{}
				err := c.Client.Get(context.Background(), types.NamespacedName{Name: e.Object.GetNamespace()}, ns)
				if err != nil {
					return false
				}

				// Check for Tenant OwnerReferences
				for _, ownerRef := range ns.ObjectMeta.OwnerReferences {
					if capsuleutils.IsTenantOwnerReference(ownerRef) {
						return true
					}
				}

				return false
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				ns := &corev1.Namespace{}
				err := c.Client.Get(context.Background(), types.NamespacedName{Name: e.ObjectNew.GetNamespace()}, ns)
				if err != nil {
					return false
				}

				// Check for Tenant OwnerReferences
				for _, ownerRef := range ns.ObjectMeta.OwnerReferences {
					if capsuleutils.IsTenantOwnerReference(ownerRef) {
						return true
					}
				}

				return false
			},
			DeleteFunc: func(_ event.DeleteEvent) bool {
				// Ignore delete events
				return false
			},
			GenericFunc: func(_ event.GenericEvent) bool {
				// Ignore generic events
				return false
			},
		})).
		Complete(c)
}
