module github.com/clastix/capsule-proxy

go 1.16

require (
	github.com/clastix/capsule v0.0.4
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/gorilla/mux v1.8.0
	go.uber.org/zap v1.15.0
	k8s.io/api v0.19.3
	k8s.io/apimachinery v0.19.3
	k8s.io/client-go v0.19.3
	sigs.k8s.io/controller-runtime v0.7.0-alpha.4
)
