// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package request

import (
	nethttp "net/http"
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
)

func SanitizeImpersonationHeaders(request *nethttp.Request) {
	request.Header.Del(authenticationv1.ImpersonateUserHeader)
	request.Header.Del(authenticationv1.ImpersonateGroupHeader)

	for header := range request.Header {
		if strings.HasPrefix(header, authenticationv1.ImpersonateUserExtraHeaderPrefix) {
			request.Header.Del(header)
		}
	}
}

func GetImpersonatingUser(request *nethttp.Request) string {
	return request.Header.Get(authenticationv1.ImpersonateUserHeader)
}

func GetImpersonatingGroups(request *nethttp.Request) []string {
	if groups := request.Header.Get(authenticationv1.ImpersonateGroupHeader); len(groups) > 0 {
		return strings.Split(groups, ",")
	}

	return nil
}
