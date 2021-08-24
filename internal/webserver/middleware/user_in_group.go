// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net/http"

	"github.com/clastix/capsule/pkg/utils"
	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	"sigs.k8s.io/controller-runtime/pkg/client"

	req "github.com/clastix/capsule-proxy/internal/request"
)

func CheckUserInCapsuleGroupMiddleware(client client.Client, log logr.Logger, claim string, groupNames []string, impersonate func(http.ResponseWriter, *http.Request)) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			_, groups, err := req.NewHTTP(request, claim, client).GetUserAndGroups()
			if err != nil {
				log.Error(err, "Cannot retrieve username and group from request")
			}
			for _, group := range groupNames {
				if utils.NewUserGroupList(groups).Find(group) || utils.NewUserGroupList(groups).Find("system:serviceaccounts") {
					next.ServeHTTP(writer, request)

					return
				}
			}
			log.V(5).Info("current user is not a Capsule one")
			impersonate(writer, request)
		})
	}
}
