package modules

import (
	"net/http"

	"github.com/clastix/capsule-proxy/internal/tenant"
	"k8s.io/apimachinery/pkg/labels"
)

type Module interface {
	Path() string
	Methods() []string
	Handle(proxyTenants []*tenant.ProxyTenant, request *http.Request) (selector labels.Selector, err error)
}
