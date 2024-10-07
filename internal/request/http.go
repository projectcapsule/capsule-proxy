// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package request

import (
	"encoding/base64"
	"fmt"
	h "net/http"
	"regexp"
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/apiserver/pkg/authentication/user"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type http struct {
	*h.Request
	authTypes                  []AuthType
	usernameClaimField         string
	ignoredImpersonationGroups []string
	impersonationGroupsRegexp  *regexp.Regexp
	skipImpersonationReview    bool
	client                     client.Writer
}

func NewHTTP(request *h.Request, authTypes []AuthType, usernameClaimField string, client client.Writer, ignoredImpersonationGroups []string, impersonationGroupsRegexp *regexp.Regexp, skipImpersonationReview bool) Request {
	return &http{Request: request, authTypes: authTypes, usernameClaimField: usernameClaimField, client: client, ignoredImpersonationGroups: ignoredImpersonationGroups, impersonationGroupsRegexp: impersonationGroupsRegexp, skipImpersonationReview: skipImpersonationReview}
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
	//nolint:nestif
	if impersonateGroups := GetImpersonatingGroups(h.Request, h.ignoredImpersonationGroups, h.impersonationGroupsRegexp); len(impersonateGroups) > 0 {
		if !h.skipImpersonationReview {
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
		}

		defer func() {
			groups = impersonateGroups
		}()
	}

	//nolint:nestif
	if impersonateUser := GetImpersonatingUser(h.Request); len(impersonateUser) > 0 {
		if !h.skipImpersonationReview {
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
		}

		// Assign impersonate user after group impersonation with current user
		// As defer func works in LIFO, if user is also impersonating groups, they will be set to correct value in the previous defer func.
		// Otherwise, groups will be set to nil, meaning we are checking just user permissions.
		defer func() {
			username = impersonateUser
			groups = nil

			// If the user is of a service account, replicate the work of the built-in service account token authenticator
			// by appending the expected service account groups:
			// - system:serviceaccounts:<namespace>
			// - system:serviceaccounts
			// - system:authenticated
			if namespace, _, err := serviceaccount.SplitUsername(username); err == nil {
				groups = append(groups, fmt.Sprintf("%s%s", serviceaccount.ServiceAccountGroupPrefix, namespace))
				groups = append(groups, serviceaccount.AllServiceAccountsGroup)
				groups = append(groups, user.AllAuthenticated)
			}
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

// Get the JWT from headers
// If there is no Authorizaion Bearer, then try finding the Bearer in Websocket Protocols header. This is for browser support.
func (h http) bearerToken() string {
	tradBearer := strings.ReplaceAll(h.Header.Get("Authorization"), "Bearer ", "")
	wsHeader := h.Header.Get("Sec-Websocket-Protocol")
	if tradBearer != "" {
		return tradBearer
	} else if wsHeader != "" {
		re := regexp.MustCompile(`(base64url\.bearer\.authorization\.k8s\.io\.)([^,]*)`)
		match := re.FindStringSubmatch(wsHeader)
		// our token is base64 encoded without padding
		b64decode, err := base64.RawStdEncoding.DecodeString(match[2])
		if err != nil {
			fmt.Println("failed to decode websocket auth bearer:", err)
		}
		if match[2] != "" {
			return string(b64decode)
		} else {
			return ""
		}
	} else {
		return ""
	}
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
