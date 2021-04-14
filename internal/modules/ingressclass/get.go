package ingressclass

import (
	"context"
	"fmt"
	"net/http"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/modules"
	"github.com/clastix/capsule-proxy/internal/modules/errors"
)

type get struct {
	client client.Client
	log    logr.Logger
}

func Get(client client.Client) modules.Module {
	return &get{client: client, log: ctrl.Log.WithName("ingressclass_get")}
}

func (g get) Path() string {
	return "/apis/networking.k8s.io/{version}/ingressclasses/{name}"
}

func (g get) Methods() []string {
	return []string{}
}

func (g get) Handle(tenantList *capsulev1alpha1.TenantList, request *http.Request) (selector labels.Selector, err error) {
	exactMatch, regexMatch := getIngressClasses(request, tenantList)

	name := mux.Vars(request)["name"]

	var ic client.ObjectList

	if ic, err = getIngressClassFromRequest(request); err != nil {
		br := errors.NewBadRequest(err, &metav1.StatusDetails{Group: "networking.k8s.io", Kind: "ingressclasses"})
		// nolint:wrapcheck
		return nil, br
	}

	if err = g.client.List(context.Background(), ic, client.MatchingLabels{"name": name}); err != nil {
		br := errors.NewBadRequest(err, &metav1.StatusDetails{Group: "networking.k8s.io", Kind: "ingressclasses"})
		// nolint:wrapcheck
		return nil, br
	}

	var r *labels.Requirement

	if r, err = getIngressClassSelector(ic, exactMatch, regexMatch); err == nil {
		return labels.NewSelector().Add(*r), nil
	}

	switch request.Method {
	case http.MethodGet:
		nf := errors.NewNotFoundError(
			fmt.Sprintf("ingressclasses.networking.k8s.io \"%s\" not found", name),
			&metav1.StatusDetails{
				Name:  name,
				Group: "networking.k8s.io",
				Kind:  "ingressclasses",
			},
		)
		// nolint:wrapcheck
		return nil, nf
	default:
		return nil, nil
	}
}
