// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package storageclass

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/modules"
	"github.com/clastix/capsule-proxy/internal/modules/errors"
	"github.com/clastix/capsule-proxy/internal/modules/utils"
	"github.com/clastix/capsule-proxy/internal/request"
	"github.com/clastix/capsule-proxy/internal/tenant"
)

type list struct {
	client client.Reader
	log    logr.Logger
}

func List(client client.Reader) modules.Module {
	return &list{client: client, log: ctrl.Log.WithName("storageclass_list")}
}

func (l list) Path() string {
	return "/apis/storage.k8s.io/v1/{endpoint:storageclasses/?}"
}

func (l list) Methods() []string {
	return []string{}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	allowed, exactMatch, regexMatch, selectorsMatch := getStorageClasses(httpRequest, proxyTenants)
	if len(selectorsMatch) > 0 {
		return utils.HandleListSelector(selectorsMatch)
	}

	kind := mux.Vars(httpRequest)["endpoint"]

	sc := &storagev1.StorageClassList{}
	if err = l.client.List(httpRequest.Context(), sc); err != nil {
		return nil, errors.NewBadRequest(err, &metav1.StatusDetails{Group: storagev1.GroupName, Kind: kind})
	}

	var r *labels.Requirement

	if r, err = getStorageClassSelector(sc, exactMatch, regexMatch); err != nil {
		if !allowed {
			return nil, errors.NewBadRequest(fmt.Errorf("not allowed"), &metav1.StatusDetails{Group: storagev1.GroupName, Kind: kind})
		}

		r, _ = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	return labels.NewSelector().Add(*r), nil
}
