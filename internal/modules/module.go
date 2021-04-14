package modules

import (
	"net/http"

	"github.com/clastix/capsule/api/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
)

type Module interface {
	Path() string
	Methods() []string
	Handle(tenantList *v1alpha1.TenantList, request *http.Request) (selector labels.Selector, err error)
}
