package storageclass

import (
	"context"
	"net/http"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/storage/v1"
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
	return &list{client: client, log: ctrl.Log.WithName("storageclass_list")}
}

func (l list) Path() string {
	return "/apis/storage.k8s.io/v1/storageclasses"
}

func (l list) Methods() []string {
	return []string{}
}

func (l list) Handle(tenantList *capsulev1alpha1.TenantList, req *http.Request) (selector labels.Selector, err error) {
	exactMatch, regexMatch := getStorageClasses(req, tenantList)

	sc := &v1.StorageClassList{}
	if err = l.client.List(context.Background(), sc); err != nil {
		return nil, errors.NewBadRequest(err, &metav1.StatusDetails{Group: "storage.k8s.io", Kind: "storageclasses"})
	}

	var r *labels.Requirement
	if r, err = getStorageClassSelector(sc, exactMatch, regexMatch); err != nil {
		r, _ = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	return labels.NewSelector().Add(*r), nil
}
