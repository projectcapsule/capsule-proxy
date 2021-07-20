package namespace

import (
	"net/http"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/modules"
	"github.com/clastix/capsule-proxy/internal/modules/errors"
	"github.com/clastix/capsule-proxy/internal/tenant"
)

type list struct {
	client client.Client
	log    logr.Logger
}

func List(client client.Client) modules.Module {
	return &list{client: client, log: ctrl.Log.WithName("namespace_list")}
}

func (l list) Path() string {
	return "/api/v1/namespaces"
}

func (l list) Methods() []string {
	return []string{http.MethodGet}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, request *http.Request) (selector labels.Selector, err error) {
	ownedTenants := make([]string, len(proxyTenants))

	for i, pt := range proxyTenants {
		ownedTenants[i] = pt.Tenant.GetName()
	}

	var capsuleLabel string

	if capsuleLabel, err = capsulev1beta1.GetTypeLabel(&capsulev1beta1.Tenant{}); err != nil {
		return nil, errors.NewBadRequest(err, &metav1.StatusDetails{Kind: "tenants"})
	}

	var r *labels.Requirement

	switch {
	case len(ownedTenants) > 0:
		r, err = labels.NewRequirement(capsuleLabel, selection.In, ownedTenants)
	default:
		r, err = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	if err != nil {
		return nil, errors.NewBadRequest(err, &metav1.StatusDetails{Kind: "namespaces"})
	}

	return labels.NewSelector().Add(*r), nil
}
