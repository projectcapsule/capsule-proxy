package request

import (
	"context"
	"fmt"
	h "net/http"
	"strings"

	"github.com/dgrijalva/jwt-go"
	authenticationv1 "k8s.io/api/authentication/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	client             client.Client
}

func NewHTTP(request *h.Request, usernameClaimField string, client client.Client) Request {
	return &http{Request: request, usernameClaimField: usernameClaimField, client: client}
}

func (h http) GetUserAndGroups() (username string, groups []string, err error) {
	switch h.getAuthType() {
	case certificateBased:
		if pc := h.TLS.PeerCertificates; len(pc) == 1 {
			username, groups = pc[0].Subject.CommonName, pc[0].Subject.Organization
		}
	case bearerBased:
		if h.isJwtToken() {
			return h.processJwtClaims()
		}

		return h.processBearerToken()
	case anonymousBased:
		return
	}

	return
}

func (h http) processJwtClaims() (username string, groups []string, err error) {
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

	return username, groups, nil
}

func (h http) processBearerToken() (username string, groups []string, err error) {
	token := h.bearerToken()
	tr := &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{
			Token: token,
		},
	}

	if err = h.client.Create(context.Background(), tr); err != nil {
		return "", nil, fmt.Errorf("cannot create TokenReview")
	}

	if statusErr := tr.Status.Error; len(statusErr) > 0 {
		return "", nil, fmt.Errorf("cannot verify the token due to error")
	}

	return tr.Status.User.Username, tr.Status.User.Groups, nil
}

func (h http) bearerToken() string {
	return strings.ReplaceAll(h.Header.Get("Authorization"), "Bearer ", "")
}

func (h http) getAuthType() authType {
	switch {
	case len(h.bearerToken()) > 0:
		return bearerBased
	case h.TLS != nil:
		return certificateBased
	default:
		return anonymousBased
	}
}

func (h http) getJwtClaims() jwt.MapClaims {
	parser := jwt.Parser{
		SkipClaimsValidation: true,
	}

	var token *jwt.Token

	var err error

	if token, _, err = parser.ParseUnverified(h.bearerToken(), jwt.MapClaims{}); err != nil {
		panic(err)
	}

	return token.Claims.(jwt.MapClaims)
}

func (h http) isJwtToken() bool {
	parser := jwt.Parser{
		SkipClaimsValidation: true,
	}
	_, _, err := parser.ParseUnverified(h.bearerToken(), jwt.MapClaims{})

	return err == nil
}
