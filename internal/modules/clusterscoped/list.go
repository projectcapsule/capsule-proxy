package clusterscoped

import (
	"slices"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	"github.com/projectcapsule/capsule-proxy/internal/modules/utils"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

type list struct {
	path   string
	log    logr.Logger
	reader client.Reader
	writer client.Writer
}

func List(client client.Reader, writer client.Writer, path string) modules.Module {
	return &list{
		path:   path,
		log:    ctrl.Log.WithName("clusterresource_list"),
		reader: client,
		writer: writer,
	}
}

func (l list) GroupKind() schema.GroupKind {
	return schema.GroupKind{}
}

func (l list) Path() string {
	return l.path
}

func (l list) Methods() []string {
	return []string{}
}

func (l list) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	gvk := utils.GetGVKFromURL(proxyRequest.GetHTTPRequest().URL.Path)

	operations, requirements := getRequirements(gvk, proxyTenants)
	if len(requirements) > 0 {
		// Verify if the list operation is allowed
		if slices.Contains(operations, v1beta1.ClusterResourceOperationList) {
			return utils.HandleListSelector(requirements)
		}

		return nil, errors.NewNotAllowed(gvk.GroupKind())
	}

	r, _ := labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})

	return labels.NewSelector().Add(*r), nil
}
