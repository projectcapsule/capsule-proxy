module github.com/clastix/capsule-proxy

go 1.16

require (
	github.com/clastix/capsule v0.1.0
	github.com/go-logr/logr v0.4.0
	github.com/golang-jwt/jwt v3.2.2+incompatible
	github.com/gorilla/handlers v1.5.1
	github.com/gorilla/mux v1.8.0
	github.com/pkg/errors v0.9.1
	github.com/spf13/pflag v1.0.5
	go.uber.org/zap v1.18.1
	k8s.io/api v0.22.0
	k8s.io/apimachinery v0.22.1
	k8s.io/apiserver v0.22.0
	k8s.io/client-go v0.22.0
	sigs.k8s.io/controller-runtime v0.9.5
)
