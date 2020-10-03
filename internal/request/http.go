package request

import (
	"fmt"
	h "net/http"
	"strings"

	"github.com/dgrijalva/jwt-go"
)

type authType int

const (
	bearerBased authType = iota
	certificateBased
	anonymousBased
)

type http struct {
	*h.Request
	usernameClaimField string
}

func NewHttp(request *h.Request, usernameClaimField string) Request {
	return &http{Request: request, usernameClaimField: usernameClaimField}
}

func (h http) IsNamespaceListing() (ok bool) {
	ok = h.URL.Path == "/api/v1/namespaces"
	ok = (h.Method == "GET" || h.isWatchEndpoint()) && ok
	return
}

func (h http) GetUserAndGroups() (username string, groups []string, err error) {
	switch h.getAuthType() {
	case certificateBased:
		if pc := h.TLS.PeerCertificates; len(pc) == 1 {
			username, groups = pc[0].Subject.CommonName, pc[0].Subject.Organization
		}
	case bearerBased:
		claims := h.getJwtClaims()

		u, ok := claims[h.usernameClaimField]
		if !ok {
			return "", nil, fmt.Errorf("missing groups claim in JWT")
		}
		username = u.(string)

		g, ok := claims["groups"]
		if !ok {
			return "", nil, fmt.Errorf("missing groups claim in JWT")
		}
		for _, v := range g.([]interface{}) {
			groups = append(groups, v.(string))
		}
	case anonymousBased:
		return
	}

	return
}

func (h http) isWatchEndpoint() (ok bool) {
	w, ok := h.URL.Query()["watch"]
	if ok && len(w) == 1 && w[0] == "true" {
		ok = true
	}
	return
}

func (h http) bearerToken() string {
	return strings.Replace(h.Header.Get("Authorization"), "Bearer ", "", -1)
}

func (h http) getAuthType() authType {
	if len(h.bearerToken()) > 0 {
		return bearerBased
	}
	if h.TLS != nil {
		return certificateBased
	}
	return anonymousBased
}

func (h http) getJwtClaims() jwt.MapClaims {
	parser := jwt.Parser{
		SkipClaimsValidation: true,
	}
	token, _, err := parser.ParseUnverified(h.bearerToken(), jwt.MapClaims{})
	if err != nil {
		panic(err)
	}

	return token.Claims.(jwt.MapClaims)
}
