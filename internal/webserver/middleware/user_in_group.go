// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/controllers"
	req "github.com/clastix/capsule-proxy/internal/request"
)

func CheckUserInIgnoredGroupMiddleware(client client.Writer, log logr.Logger, claim string, authTypes []req.AuthType, ignoredUserGroups sets.String, fn func(writer http.ResponseWriter, request *http.Request)) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if ignoredUserGroups.Len() > 0 {
				user, groups, err := req.NewHTTP(request, authTypes, claim, client).GetUserAndGroups()
				if err != nil {
					log.Error(err, "Cannot retrieve username and group from request")
				}

				for _, group := range groups {
					if ignoredUserGroups.Has(group) {
						log.V(5).Info("current user belongs to ignored groups", "user", user)
						fn(writer, request)

						return
					}
				}
			}

			next.ServeHTTP(writer, request)
		})
	}
}

func CheckUserInCapsuleGroupMiddleware(client client.Writer, log logr.Logger, claim string, authTypes []req.AuthType, impersonate func(http.ResponseWriter, *http.Request)) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			_, groups, err := req.NewHTTP(request, authTypes, claim, client).GetUserAndGroups()
			if err != nil {
				log.Error(err, "Cannot retrieve username and group from request")
			}
			for _, group := range groups {
				if controllers.CapsuleUserGroups.Has(group) {
					next.ServeHTTP(writer, request)

					return
				}
			}
			log.V(5).Info("current user is not a Capsule one")
			impersonate(writer, request)
		})
	}
}
