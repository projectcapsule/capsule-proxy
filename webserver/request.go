package webserver

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/clastix/capsule/pkg/utils"
	"github.com/dgrijalva/jwt-go"
)

type Request interface {
	IsUserInGroup(groupName string) (bool, error)
	GetUserName() (string, error)
}

type httpRequest struct {
	*http.Request
}

func (h httpRequest) getJwtClaims() jwt.MapClaims {
	parser := jwt.Parser{
		SkipClaimsValidation: true,
	}
	token, _, err := parser.ParseUnverified(strings.Replace(h.Header.Get("Authorization"), "Bearer ", "", -1), jwt.MapClaims{})
	if err != nil {
		panic(err)
	}

	return token.Claims.(jwt.MapClaims)
}

func (h httpRequest) IsUserInGroup(groupName string) (bool, error) {
	claims := h.getJwtClaims()
	g, ok := claims["groups"]
	if !ok {
		return false, fmt.Errorf("missing groups claim in JWT")
	}
	var groups []string
	for _, v := range g.([]interface{}) {
		groups = append(groups, v.(string))
	}
	return utils.UserGroupList(groups).IsInCapsuleGroup(groupName), nil
}

func (h httpRequest) GetUserName() (string, error) {
	claims := h.getJwtClaims()
	username, ok := claims["preferred_username"]
	if !ok {
		return "", fmt.Errorf("missing groups claim in JWT")
	}
	return username.(string), nil
}

func NewHttpRequest(request *http.Request) Request {
	return &httpRequest{Request: request}
}
