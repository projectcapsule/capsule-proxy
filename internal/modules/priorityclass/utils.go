// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package priorityclass

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	"github.com/clastix/capsule-proxy/internal/tenant"
)

func getPriorityClass(req *http.Request, proxyTenants []*tenant.ProxyTenant) (allowed bool, exact []string, regex []*regexp.Regexp, requirements []labels.Requirement) {
	requirements = []labels.Requirement{}

	for _, pt := range proxyTenants {
		if ok := pt.RequestAllowed(req, capsulev1beta2.PriorityClassesProxy); !ok {
			continue
		}

		allowed = true

		pc := pt.Tenant.Spec.PriorityClasses
		if pc == nil {
			continue
		}

		if len(pc.SelectorAllowedListSpec.Exact) > 0 {
			exact = append(exact, pc.SelectorAllowedListSpec.Exact...)
		}

		if len(pc.Default) > 0 {
			exact = append(exact, pc.Default)
		}

		if r := pc.SelectorAllowedListSpec.Regex; len(r) > 0 {
			regex = append(regex, regexp.MustCompile(r))
		}

		selector, err := metav1.LabelSelectorAsSelector(&pc.SelectorAllowedListSpec.LabelSelector)
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

func getPriorityClassSelector(classes *schedulingv1.PriorityClassList, exact []string, regex []*regexp.Regexp) (*labels.Requirement, error) {
	isPriorityClassRegexed := func(name string, regex []*regexp.Regexp) bool {
		for _, r := range regex {
			if r.MatchString(name) {
				return true
			}
		}

		return false
	}

	var names []string

	for _, s := range classes.Items {
		if isPriorityClassRegexed(s.GetName(), regex) {
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

	return nil, fmt.Errorf("cannot create LabelSelector for the requested PriorityClass requirement")
}
