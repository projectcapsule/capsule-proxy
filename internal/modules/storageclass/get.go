// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package storageclass

import (
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	"github.com/projectcapsule/capsule-proxy/internal/modules/utils"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

type get struct {
	client client.Reader
	log    logr.Logger
	gk     schema.GroupVersionKind
}

func Get(client client.Reader) modules.Module {
	return &get{
		client: client,
		log:    ctrl.Log.WithName("storageclass_get"),
		gk: schema.GroupVersionKind{
			Group:   storagev1.GroupName,
			Version: "*",
			Kind:    "storageclasses",
		},
	}
}

func (g get) GroupVersionKind() schema.GroupVersionKind {
	return g.gk
}

func (g get) GroupKind() schema.GroupKind {
	return g.gk.GroupKind()
}

func (g get) Path() string {
	return "/apis/storage.k8s.io/v1/{endpoint:storageclasses}/{name}"
}

func (g get) Methods() []string {
	return []string{}
}

func (g get) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	name := mux.Vars(httpRequest)["name"]

	_, exactMatch, regexMatch, requirements := getStorageClasses(httpRequest, proxyTenants)
	if len(requirements) > 0 {
		sc := &storagev1.StorageClass{}

		return utils.HandleGetSelector(httpRequest.Context(), sc, g.client, requirements, name, g.GroupKind())
	}

	sc := &storagev1.StorageClassList{}
	if err = g.client.List(httpRequest.Context(), sc, client.MatchingLabels{corev1.LabelMetadataName: name}); err != nil {
		return nil, errors.NewBadRequest(err, g.GroupKind())
	}

	var r *labels.Requirement

	r, err = getStorageClassSelector(sc, exactMatch, regexMatch)

	switch {
	case err == nil:
		return labels.NewSelector().Add(*r), nil
	case httpRequest.Method == http.MethodGet:
		return nil, errors.NewNotFoundError(name, g.GroupKind())
	default:
		return nil, nil
	}
}
