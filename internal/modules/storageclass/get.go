// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package storageclass

import (
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/modules"
	"github.com/clastix/capsule-proxy/internal/modules/errors"
	"github.com/clastix/capsule-proxy/internal/modules/utils"
	"github.com/clastix/capsule-proxy/internal/request"
	"github.com/clastix/capsule-proxy/internal/tenant"
)

type get struct {
	client client.Reader
	log    logr.Logger
}

func Get(client client.Reader) modules.Module {
	return &get{client: client, log: ctrl.Log.WithName("storageclass_get")}
}

func (g get) Path() string {
	return "/apis/storage.k8s.io/v1/{endpoint:storageclasses}/{name}"
}

func (g get) Methods() []string {
	return []string{}
}

func (g get) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	name, kind := mux.Vars(httpRequest)["name"], mux.Vars(httpRequest)["endpoint"]

	_, exactMatch, regexMatch, requirements := getStorageClasses(httpRequest, proxyTenants)
	if len(requirements) > 0 {
		sc := &storagev1.StorageClass{}
		sc.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   storagev1.GroupName,
			Version: storagev1.SchemeGroupVersion.Version,
			Kind:    kind,
		})

		return utils.HandleGetSelector(httpRequest.Context(), sc, g.client, requirements, name, kind)
	}

	sc := &storagev1.StorageClassList{}
	if err = g.client.List(httpRequest.Context(), sc, client.MatchingLabels{"name": name}); err != nil {
		return nil, errors.NewBadRequest(
			err,
			&metav1.StatusDetails{
				Group: storagev1.GroupName,
				Kind:  kind,
			},
		)
	}

	var r *labels.Requirement
	r, err = getStorageClassSelector(sc, exactMatch, regexMatch)

	switch {
	case err == nil:
		return labels.NewSelector().Add(*r), nil
	case httpRequest.Method == http.MethodGet:
		return nil, g.notFound(name, kind)
	default:
		return nil, nil
	}
}

func (g get) notFound(name, kind string) error {
	return errors.NewNotFoundError(
		fmt.Sprintf("%s.%s %q not found", kind, storagev1.GroupName, name),
		&metav1.StatusDetails{
			Name:  name,
			Group: storagev1.GroupName,
			Kind:  kind,
		},
	)
}
