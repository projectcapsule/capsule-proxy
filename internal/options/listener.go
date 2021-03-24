package options

import (
	"net/http"
	"net/url"
)

type ListenerOpts interface {
	KubernetesControlPlaneURL() *url.URL
	UserGroupName() string
	PreferredUsernameClaim() string
	ReverseProxyTransport() (*http.Transport, error)
	BearerToken() string
}
