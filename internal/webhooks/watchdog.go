// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-logr/logr"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulelabels "github.com/projectcapsule/capsule-proxy/internal/labels"
)

// MutatingWebhook handles mutating webhook requests.
type WatchdogWebhook struct {
	Decoder admission.Decoder
	Client  client.Client
	Log     logr.Logger
}

// Handle processes the admission request and adds a label if necessary.
func (mw *WatchdogWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	mw.Log.V(7).Info("Received Request")
	// Only consider namespaced objects
	if req.Namespace == "" {
		return admission.Allowed("not namespaced object")
	}

	// Decode the object
	obj := &unstructured.Unstructured{}
	if err := mw.Decoder.Decode(req, obj); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	tntList := capsulev1beta2.TenantList{}
	if err := mw.Client.List(ctx, &tntList, client.MatchingFields{".status.namespaces": obj.GetNamespace()}); err != nil {
		admission.Errored(http.StatusInternalServerError, err)
	}

	if len(tntList.Items) == 0 {
		return admission.Allowed("no tenant object")
	}

	tenant := tntList.Items[0].Name

	mw.Log.V(7).Info("matching tenant", "name", tenant)

	// Add the label if not present
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	if currentValue, exists := labels[capsulelabels.ManagedByCapsuleLabel]; exists && currentValue == tenant {
		mw.Log.V(7).Info("label is already correctly set", capsulelabels.ManagedByCapsuleLabel, currentValue)

		return admission.Allowed("tenant already set correctly")
	}

	// Add Label
	labels[capsulelabels.ManagedByCapsuleLabel] = tntList.Items[0].Name
	obj.SetLabels(labels)

	mw.Log.V(7).Info("added label", capsulelabels.ManagedByCapsuleLabel, tntList.Items[0].Name)

	// Marshal the object back to JSON
	marshaledObj, err := json.Marshal(obj)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
}
