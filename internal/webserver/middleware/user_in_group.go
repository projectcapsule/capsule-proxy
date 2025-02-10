// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net/http"
	"regexp"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/controllers"
	req "github.com/projectcapsule/capsule-proxy/internal/request"
)

func CheckUserInIgnoredGroupMiddleware(client client.Writer, log logr.Logger, claim string, authTypes []req.AuthType, ignoredUserGroups sets.Set[string], ignoredImpersonationGroups []string, impersonationGroupsRegexp *regexp.Regexp, skipImpersonationReview bool, fn func(writer http.ResponseWriter, request *http.Request)) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if ignoredUserGroups.Len() > 0 {
				user, groups, err := req.NewHTTP(request, authTypes, claim, client, ignoredImpersonationGroups, impersonationGroupsRegexp, skipImpersonationReview).GetUserAndGroups()
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

func CheckUserInCapsuleGroupMiddleware(client client.Writer, log logr.Logger, claim string, authTypes []req.AuthType, ignoredImpersonationGroups []string, impersonationGroupsRegexp *regexp.Regexp, skipImpersonationReview bool, impersonate func(http.ResponseWriter, *http.Request)) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			_, groups, err := req.NewHTTP(request, authTypes, claim, client, ignoredImpersonationGroups, impersonationGroupsRegexp, skipImpersonationReview).GetUserAndGroups()
			if err != nil {
				log.Error(err, "Cannot retrieve username and group from request")
			}

			log.V(10).Info("request groups", "groups", groups)

			for _, group := range groups {
				if controllers.CapsuleUserGroups.Has(group) {
					next.ServeHTTP(writer, request)

					return
				}
			}

			log.V(5).Info("current user is not a Capsule one", "capsule-groups", controllers.CapsuleUserGroups)
			impersonate(writer, request)
		})
	}
}
