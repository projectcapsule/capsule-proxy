package namespaced

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulelabels "github.com/projectcapsule/capsule-proxy/internal/labels"
	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

type catchall struct {
	path   string
	reader client.Reader
	writer client.Writer
}

func CatchAll(client client.Reader, writer client.Writer, path string) modules.Module {
	return &catchall{
		path:   path,
		reader: client,
		writer: writer,
	}
}

func (l catchall) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{}
}

func (l catchall) GroupKind() schema.GroupKind {
	return schema.GroupKind{}
}

func (l catchall) Path() string {
	return l.path
}

func (l catchall) Methods() []string {
	return []string{"get"}
}

func (l catchall) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	user, groups, _ := proxyRequest.GetUserAndGroups()

	var group, version, kind string

	url := proxyRequest.GetHTTPRequest().URL.Path

	parts := strings.Split(url, "/")

	switch len(parts) {
	case 5:
		group = parts[2]
		version = parts[3]
		kind = parts[4]
	case 4:
		version = parts[2]
		kind = parts[3]
	}

	var sourceTenants []string

	for _, tnt := range proxyTenants {
		var allowed bool

		for _, ns := range tnt.Tenant.Status.Namespaces {
			sar := v1.SubjectAccessReview{}
			sar.Spec.User = user
			sar.Spec.Groups = groups
			sar.Spec.ResourceAttributes = &v1.ResourceAttributes{
				Namespace: ns,
				Verb:      "list",
				Group:     group,
				Version:   version,
				Resource:  kind,
			}

			if err = l.writer.Create(proxyRequest.GetHTTPRequest().Context(), &sar); err != nil {
				return nil, fmt.Errorf("unable to check if user can list %s/%s", group, kind)
			}

			allowed = sar.Status.Allowed

			break
		}

		if allowed {
			sourceTenants = append(sourceTenants, tnt.Tenant.Name)
		}
	}

	var r *labels.Requirement

	switch {
	case len(sourceTenants) > 0:
		r, err = labels.NewRequirement(capsulelabels.ManagedByCapsuleLabel, selection.In, sourceTenants)
	default:
		r, err = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	return labels.NewSelector().Add(*r), err
}
