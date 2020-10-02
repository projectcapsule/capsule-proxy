package options

import (
	"net/http"
	"net/url"
)

type ListenerOpts interface {
	KubernetesControlPlaneUrl() *url.URL
	UserGroupName() string
	PreferredUsernameClaim() string
	ReverseProxyTransport() *http.Transport
	BearerToken() string
}
