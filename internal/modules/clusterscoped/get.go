// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package clusterscoped

import (
	"context"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	"github.com/projectcapsule/capsule-proxy/internal/modules/utils"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

type get struct {
	path      string
	log       logr.Logger
	discovery *discovery.DiscoveryClient
	reader    client.Reader
	writer    client.Writer
}

func Get(discovery *discovery.DiscoveryClient, client client.Reader, writer client.Writer, path string) modules.Module {
	return &get{
		path:      path,
		log:       ctrl.Log.WithName("clusterresource_get"),
		discovery: discovery,
		reader:    client,
		writer:    writer,
	}
}

func (g get) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{}
}

func (g get) GroupKind() schema.GroupKind {
	return schema.GroupKind{}
}

func (g get) Path() string {
	return g.path
}

func (g get) Methods() []string {
	return []string{}
}

func (g get) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	httpRequest := proxyRequest.GetHTTPRequest()

	gvk := utils.GetGVKFromURL(proxyRequest.GetHTTPRequest().URL.Path)

	_, requirements := utils.GetClusterScopeRequirements(gvk, proxyTenants)

	if len(requirements) > 0 {
		switch httpRequest.Method {
		case http.MethodGet:
			return g.handleSelector(httpRequest.Context(), gvk, requirements, mux.Vars(httpRequest)["name"])
		default:
			return nil, nil
		}
	}

	return
}

func (g get) handleSelector(ctx context.Context, gvk *schema.GroupVersionKind, requirements []labels.Requirement, name string) (selector labels.Selector, err error) {
	err = utils.ReplacePluralWithKind(g.discovery, gvk)
	if err != nil {
		return nil, err
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(*gvk)

	if err := g.reader.Get(ctx, types.NamespacedName{Name: name}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errors.NewNotFoundError(name, gvk.GroupKind())
		}

		return nil, err
	}

	selector = labels.NewSelector()

	for _, requirement := range requirements {
		if requirement.Matches(labels.Set(obj.GetLabels())) {
			return selector.Add(requirement), nil
		}
	}

	return nil, nil
}
