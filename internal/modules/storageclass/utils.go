// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package storageclass

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	"github.com/clastix/capsule-proxy/internal/tenant"
)

func getStorageClasses(req *http.Request, proxyTenants []*tenant.ProxyTenant) (allowed bool, exact []string, regex []*regexp.Regexp, requirements []labels.Requirement) {
	requirements = []labels.Requirement{}

	for _, pt := range proxyTenants {
		if ok := pt.RequestAllowed(req, capsulev1beta2.StorageClassesProxy); !ok {
			continue
		}

		allowed = true

		sc := pt.Tenant.Spec.StorageClasses
		if sc == nil {
			continue
		}

		if len(sc.SelectorAllowedListSpec.Exact) > 0 {
			exact = append(exact, sc.SelectorAllowedListSpec.Exact...)
		}

		if len(sc.Default) > 0 {
			exact = append(exact, sc.Default)
		}

		if r := sc.SelectorAllowedListSpec.Regex; len(r) > 0 {
			regex = append(regex, regexp.MustCompile(r))
		}

		selector, err := metav1.LabelSelectorAsSelector(&sc.SelectorAllowedListSpec.LabelSelector)
		if err != nil {
			continue
		}

		reqs, selectable := selector.Requirements()
		if !selectable {
			continue
		}

		requirements = append(requirements, reqs...)
	}

	sort.SliceStable(exact, func(i, j int) bool {
		return exact[i] < exact[0]
	})

	return allowed, exact, regex, requirements
}

func getStorageClassSelector(classes *storagev1.StorageClassList, exact []string, regex []*regexp.Regexp) (*labels.Requirement, error) {
	isStorageClassRegexed := func(name string, regex []*regexp.Regexp) bool {
		for _, r := range regex {
			if r.MatchString(name) {
				return true
			}
		}

		return false
	}

	var names []string

	for _, s := range classes.Items {
		if isStorageClassRegexed(s.GetName(), regex) {
			names = append(names, s.GetName())

			continue
		}

		if f := sort.SearchStrings(exact, s.GetName()); f < len(exact) && exact[f] == s.GetName() {
			names = append(names, s.GetName())
		}
	}

	if len(names) > 0 {
		return labels.NewRequirement("name", selection.In, names)
	}

	return nil, fmt.Errorf("cannot create LabelSelector for the requested StorageClass requirement")
}
