// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package priorityclass

import (
	"github.com/go-logr/logr"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	gk     schema.GroupKind
}

func List(client client.Reader) modules.Module {
	return &list{
		client: client,
		log:    ctrl.Log.WithName("priorityclass_list"),
		gk: schema.GroupKind{
			Group: schedulingv1.GroupName,
			Kind:  "priorityclasses",
		},
	}
}

func (l list) Path() string {
	return "/apis/scheduling.k8s.io/v1/{endpoint:priorityclasses/?}"
}

func (l list) Methods() []string {
	return []string{}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	allowed, exactMatch, regexMatch, selectorsMatch := getPriorityClass(httpRequest, proxyTenants)
	if len(selectorsMatch) > 0 {
		return utils.HandleListSelector(selectorsMatch)
	}

	sc := &schedulingv1.PriorityClassList{}
	if err = l.client.List(httpRequest.Context(), sc); err != nil {
		return nil, errors.NewBadRequest(err, l.gk)
	}

	var r *labels.Requirement

	if r, err = getPriorityClassSelector(sc, exactMatch, regexMatch); err != nil {
		if !allowed {
			return nil, errors.NewNotAllowed(l.gk)
		}

		r, _ = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	return labels.NewSelector().Add(*r), nil
}
