// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/webserver/errors"
)

const (
	regexPatternForAuthHeader = "^(Bearer ([\\w-]*\\.[\\w-]*\\.[\\w-]*))$"
)

func CheckAuthorization(client client.Client, log logr.Logger) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			err := fmt.Errorf("no authentication method")
			isCertificates := request.TLS != nil && len(request.TLS.PeerCertificates) > 0

			isBearerToken, errBT := checkBearerToken(request.Header.Get("Authorization"))
			if errBT != nil {
				errors.HandleUnauthorized(writer, err, "authorization header does not contain valid data.")
			}

			if !isCertificates && !isBearerToken {
				errors.HandleUnauthorized(writer, err, "cannot determinate the current user due to no cert-based authentication nor valid JWT token.")
			}

			next.ServeHTTP(writer, request)
		})
	}
}

func checkBearerToken(authorizationHeader string) (bool, error) {
	if authorizationHeader == "" {
		return false, nil
	}

	return regexp.MatchString(regexPatternForAuthHeader, authorizationHeader)
}
