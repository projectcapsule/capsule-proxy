// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/webserver/errors"
)

func CheckJWTMiddleware(client client.Writer) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			var err error

			token := strings.ReplaceAll(request.Header.Get("Authorization"), "Bearer ", "")

			if len(token) > 0 {
				tr := authenticationv1.TokenReview{
					TypeMeta: metav1.TypeMeta{
						Kind:       "TokenReview",
						APIVersion: "authentication.k8s.io/v1",
					},
					Spec: authenticationv1.TokenReviewSpec{
						Token: token,
					},
				}
				if err = client.Create(request.Context(), &tr); err != nil {
					errors.HandleError(writer, err, "cannot create TokenReview")
				}
				if statusErr := tr.Status.Error; len(statusErr) > 0 {
					errors.HandleUnauthorized(writer, fmt.Errorf(statusErr), "cannot authenticate the token due to error")
				}
			}

			next.ServeHTTP(writer, request)
		})
	}
}
