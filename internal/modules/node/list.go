package node

import (
	"context"
	"net/http"

	"github.com/clastix/capsule-proxy/internal/tenant"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/modules"
	"github.com/clastix/capsule-proxy/internal/modules/errors"
)

type list struct {
	client client.Client
	log    logr.Logger
}

func List(client client.Client) modules.Module {
	return &list{client: client, log: ctrl.Log.WithName("node_list")}
}

func (l list) Path() string {
	return "/api/v1/nodes"
}

func (l list) Methods() []string {
	return []string{}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, request *http.Request) (selector labels.Selector, err error) {
	selectors := getNodeSelectors(request, proxyTenants)

	nl := &corev1.NodeList{}
	if err = l.client.List(context.Background(), nl); err != nil {
		return nil, errors.NewBadRequest(err, &metav1.StatusDetails{Kind: "nodes"})
	}

	var r *labels.Requirement
	if r, err = getNodeSelector(nl, selectors); err != nil {
		r, _ = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	return labels.NewSelector().Add(*r), nil
}
