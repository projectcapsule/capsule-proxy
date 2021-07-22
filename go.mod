module github.com/clastix/capsule-proxy

go 1.16

require (
	github.com/clastix/capsule v0.1.0-rc4.0.20210721144813-c0d4aab582b7
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/go-logr/logr v0.3.0
	github.com/gorilla/handlers v1.5.1
	github.com/gorilla/mux v1.8.0
	github.com/pkg/errors v0.9.1
	go.uber.org/zap v1.15.0
	k8s.io/api v0.20.2
	k8s.io/apimachinery v0.20.2
	k8s.io/client-go v0.20.2
	sigs.k8s.io/controller-runtime v0.8.3
)
