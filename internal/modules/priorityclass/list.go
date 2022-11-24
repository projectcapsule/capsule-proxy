// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package priorityclass

import (
	"context"

	"github.com/go-logr/logr"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/modules"
	"github.com/clastix/capsule-proxy/internal/modules/errors"
	"github.com/clastix/capsule-proxy/internal/request"
	"github.com/clastix/capsule-proxy/internal/tenant"
)

type list struct {
	client client.Client
	log    logr.Logger
}

func List(client client.Client) modules.Module {
	return &list{client: client, log: ctrl.Log.WithName("priorityclass_list")}
}

func (l list) Path() string {
	return "/apis/scheduling.k8s.io/v1/{priorityclasses:[^/]+/?}"
}

func (l list) Methods() []string {
	return []string{}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	exactMatch, regexMatch := getPriorityClass(httpRequest, proxyTenants)

	sc := &schedulingv1.PriorityClassList{}
	if err = l.client.List(context.Background(), sc); err != nil {
		return nil, errors.NewBadRequest(err, &metav1.StatusDetails{Group: "scheduling.k8s.io", Kind: "priorityclasses"})
	}

	var r *labels.Requirement
	if r, err = getPriorityClassSelector(sc, exactMatch, regexMatch); err != nil {
		r, _ = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	return labels.NewSelector().Add(*r), nil
}
