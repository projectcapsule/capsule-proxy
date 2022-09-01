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
	regexPatternForAuthHeader = "^(Bearer ([\\w-]*\\.[\\w-]*\\.[\\w-]*|[\\w-]*|[\\w-]*\\.[\\w-]*))$"
)

func CheckAuthorization(client client.Client, log logr.Logger, tls bool) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			err := fmt.Errorf("forbidden access")

			isCertificates := request.TLS != nil && len(request.TLS.PeerCertificates) > 0

			isBearerToken, errBT := CheckBearerToken(request.Header.Get("Authorization"))

			unauthorized := errBT != nil || (tls && (!isCertificates && !isBearerToken)) || (!tls && !isBearerToken)

			if unauthorized {
				errors.HandleUnauthorized(writer, err, "unauthorized")
			}

			next.ServeHTTP(writer, request)
		})
	}
}

func CheckBearerToken(authorizationHeader string) (bool, error) {
	if authorizationHeader == "" {
		return false, nil
	}

	return regexp.MatchString(regexPatternForAuthHeader, authorizationHeader)
}
