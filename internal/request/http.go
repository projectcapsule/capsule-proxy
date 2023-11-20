// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package request

import (
	"fmt"
	h "net/http"
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type http struct {
	*h.Request
	authTypes          []AuthType
	usernameClaimField string
	client             client.Writer
}

func NewHTTP(request *h.Request, authTypes []AuthType, usernameClaimField string, client client.Writer) Request {
	return &http{Request: request, authTypes: authTypes, usernameClaimField: usernameClaimField, client: client}
}

func (h http) GetHTTPRequest() *h.Request {
	return h.Request
}

//nolint:funlen
func (h http) GetUserAndGroups() (username string, groups []string, err error) {
	for _, fn := range h.authenticationFns() {
		// User authentication data is extracted according to the preferred order:
		// in case of first match blocking the iteration
		if username, groups, err = fn(); err == nil {
			break
		}
	}
	// In case of error, we're blocking the request flow here
	if err != nil {
		return "", nil, err
	}

	// In case the requester is asking for impersonation, we have to be sure that's allowed by creating a
	// SubjectAccessReview with the requested data, before proceeding.
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
		}

		defer func() {
			groups = impersonateGroups
		}()
	}

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
		// As defer func works in LIFO, if user is also impersonating groups, they will be set to correct value in the previous defer func.
		// Otherwise, groups will be set to nil, meaning we are checking just user permissions.
		defer func() {
			username = impersonateUser
			groups = nil
		}()
	}

	return username, groups, nil
}

func (h http) processBearerToken() (username string, groups []string, err error) {
	tr := &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{
			Token: h.bearerToken(),
		},
	}

	if err = h.client.Create(h.Request.Context(), tr); err != nil {
		return "", nil, fmt.Errorf("cannot create TokenReview")
	}

	if !tr.Status.Authenticated {
		return "", nil, fmt.Errorf("cannot verify the token due to error")
	}

	if statusErr := tr.Status.Error; len(statusErr) > 0 {
		return "", nil, fmt.Errorf("cannot verify the token due to error")
	}

	return tr.Status.User.Username, tr.Status.User.Groups, nil
}

func (h http) bearerToken() string {
	return strings.ReplaceAll(h.Header.Get("Authorization"), "Bearer ", "")
}

type authenticationFn func() (username string, groups []string, err error)

func (h http) authenticationFns() []authenticationFn {
	fns := make([]authenticationFn, 0, len(h.authTypes)+1)

	for _, authType := range h.authTypes {
		//nolint:exhaustive
		switch authType {
		case BearerToken:
			fns = append(fns, func() (username string, groups []string, err error) {
				if len(h.bearerToken()) == 0 {
					return "", nil, NewErrUnauthorized("unauthenticated users not supported")
				}

				return h.processBearerToken()
			})
		case TLSCertificate:
			// If the proxy is handling a non TLS connection, we have to skip the authentication strategy,
			// since the TLS section of the request would be nil.
			if h.TLS == nil {
				break
			}

			fns = append(fns, func() (username string, groups []string, err error) {
				if pc := h.TLS.PeerCertificates; len(pc) == 0 {
					err = NewErrUnauthorized("no provided peer certificates")
				} else {
					username, groups = pc[0].Subject.CommonName, pc[0].Subject.Organization
				}

				return
			})
		}
	}
	// Dead man switch, if no strategy worked, the proxy cannot work
	fns = append(fns, func() (string, []string, error) {
		return "", nil, NewErrUnauthorized("unauthenticated users not supported")
	})

	return fns
}
