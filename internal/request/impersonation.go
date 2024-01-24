// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package request

import (
	nethttp "net/http"
	"regexp"
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

func GetImpersonatingGroups(request *nethttp.Request, ignoreImpersonationGroups []string, impersonationGroupsRegexp *regexp.Regexp) []string {
	groups := request.Header.Values(authenticationv1.ImpersonateGroupHeader)
	if len(groups) > 0 {
		if impersonationGroupsRegexp != nil {
			groups = filterGroups(groups, impersonationGroupsRegexp)
		}

		if len(ignoreImpersonationGroups) > 0 {
			groups = ignoreGroups(groups, regexp.MustCompile(strings.Join(ignoreImpersonationGroups, "|")))
		}
	}

	return groups
}

func filterGroups(groups []string, impersonationGroupsRegexp *regexp.Regexp) []string {
	filteredGroups := []string{}

	for _, group := range groups {
		if impersonationGroupsRegexp.MatchString(group) {
			filteredGroups = append(filteredGroups, group)
		}
	}

	return filteredGroups
}

func ignoreGroups(groups []string, ignoredGroupsRegexp *regexp.Regexp) []string {
	ignoredGroups := []string{}

	for _, group := range groups {
		if !ignoredGroupsRegexp.MatchString(group) {
			// If the group does NOT match the regex, include it in the filtered list
			ignoredGroups = append(ignoredGroups, group)
		}
	}

	return ignoredGroups
}
