// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	goerrors "github.com/pkg/errors"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/webserver/errors"
)

func CheckJWTMiddleware(client client.Writer) mux.MiddlewareFunc {
	var mu sync.RWMutex

	invalidatedToken := sets.New[string]()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			var err error

			token := strings.ReplaceAll(request.Header.Get("Authorization"), "Bearer ", "")

			mu.RLock()
			tokenInvalidated := invalidatedToken.Has(token)
			mu.RUnlock()

			switch {
			case len(token) > 0 && !tokenInvalidated:
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

					return
				}

				if statusErr := tr.Status.Error; len(statusErr) > 0 {
					mu.Lock()
					invalidatedToken.Insert(token)
					mu.Unlock()

					errors.HandleUnauthorized(writer, goerrors.New(statusErr), "cannot authenticate the token due to error")

					return
				}
			case tokenInvalidated:
				errors.HandleUnauthorized(writer, goerrors.New("token is invalid"), "cannot authenticate the token due to error")

				return
			}

			next.ServeHTTP(writer, request)
		})
	}
}
