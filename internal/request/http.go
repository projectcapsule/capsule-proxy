// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package request

import (
	"context"
	"fmt"
	h "net/http"
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

type http struct {
	*h.Request
	authTypes          []AuthType
	usernameClaimField string
	client             Client
}

func NewHTTP(request *h.Request, authTypes []AuthType, usernameClaimField string, client Client) Request {
	return &http{Request: request, authTypes: authTypes, usernameClaimField: usernameClaimField, client: client}
}

func (h http) GetHTTPRequest() *h.Request {
	return h.Request
}

//nolint:funlen
func (h http) GetUserAndGroups() (username string, groups []string, err error) {
	switch h.getAuthType() {
	case TLSCertificate:
		pc := h.TLS.PeerCertificates
		if len(pc) == 0 {
			return "", nil, fmt.Errorf("no provided peer certificates")
		}

		username, groups = pc[0].Subject.CommonName, pc[0].Subject.Organization
	case BearerToken:
		username, groups, err = h.processBearerToken()
	case Anonymous:
		return "", nil, fmt.Errorf("capsule does not support unauthenticated users")
	}
	// In case of error, we're blocking the request flow here
	if err != nil {
		return "", nil, err
	}
	// In case the requester is asking for impersonation, we have to be sure that's allowed by creating a
	// SubjectAccessReview with the requested data, before proceeding.
	if impersonateUser := GetImpersonatingUser(h.Request); len(impersonateUser) > 0 {
		ac := &authorizationv1.SubjectAccessReview{
			Spec: authorizationv1.SubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Verb:     "impersonate",
					Resource: "users",
					Name:     impersonateUser,
				},
				User:   username,
				Groups: groups,
			},
		}
		if err = h.client.Create(h.Request.Context(), ac); err != nil {
			return "", nil, err
		}

		if !ac.Status.Allowed {
			return "", nil, NewErrUnauthorized(fmt.Sprintf("the current user %s cannot impersonate the user %s", username, impersonateUser))
		}
		// Assign impersonate user after group impersonation with current user
		defer func() {
			username = impersonateUser
		}()
	}

	if impersonateGroups := GetImpersonatingGroups(h.Request); len(impersonateGroups) > 0 {
		for _, impersonateGroup := range impersonateGroups {
			ac := &authorizationv1.SubjectAccessReview{
				Spec: authorizationv1.SubjectAccessReviewSpec{
					ResourceAttributes: &authorizationv1.ResourceAttributes{
						Verb:     "impersonate",
						Resource: "groups",
						Name:     impersonateGroup,
					},
					User:   username,
					Groups: groups,
				},
			}
			if err = h.client.Create(h.Request.Context(), ac); err != nil {
				return "", nil, err
			}

			if !ac.Status.Allowed {
				return "", nil, NewErrUnauthorized(fmt.Sprintf("the current user %s cannot impersonate the group %s", username, impersonateGroup))
			}

			if !sets.NewString(groups...).Has(impersonateGroup) {
				// The current user is allowed to perform authentication, allowing the override
				groups = append(groups, impersonateGroup)
			}
		}
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

func (h http) getAuthType() AuthType {
	for _, authType := range h.authTypes {
		// nolint:exhaustive
		switch authType {
		case BearerToken:
			if len(h.bearerToken()) > 0 {
				return BearerToken
			}
		case TLSCertificate:
			if (h.TLS != nil) && len(h.TLS.PeerCertificates) > 0 {
				return TLSCertificate
			}
		}
	}

	return Anonymous
}
