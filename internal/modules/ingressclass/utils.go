// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingressclass

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/gorilla/mux"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/tenant"
)

func getIngressClasses(request *http.Request, proxyTenants []*tenant.ProxyTenant) (allowed bool, exact []string, regex []*regexp.Regexp, requirements []labels.Requirement) {
	requirements = []labels.Requirement{}

	for _, pt := range proxyTenants {
		if ok := pt.RequestAllowed(request, capsulev1beta2.IngressClassesProxy); !ok {
			continue
		}

		allowed = true

		ic := pt.Tenant.Spec.IngressOptions.AllowedClasses
		if ic == nil {
			continue
		}

		if len(ic.SelectorAllowedListSpec.Exact) > 0 {
			exact = append(exact, ic.SelectorAllowedListSpec.Exact...)
		}

		if len(ic.Default) > 0 {
			exact = append(exact, ic.Default)
		}

		if r := ic.SelectorAllowedListSpec.Regex; len(r) > 0 {
			regex = append(regex, regexp.MustCompile(r))
		}

		selector, err := metav1.LabelSelectorAsSelector(&ic.SelectorAllowedListSpec.LabelSelector)
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

func getIngressClassListFromRequest(request *http.Request) (ic client.ObjectList, err error) {
	v := mux.Vars(request)["version"]
	switch v {
	case networkingv1.SchemeGroupVersion.Version:
		ic = &networkingv1.IngressClassList{}
	case networkingv1beta1.SchemeGroupVersion.Version:
		ic = &networkingv1beta1.IngressClassList{}
	default:
		return nil, fmt.Errorf("ingressClass %s is not supported", v)
	}

	return
}

func getIngressClassFromRequest(request *http.Request) (ic client.Object, err error) {
	v := mux.Vars(request)["version"]
	switch v {
	case "v1":
		ic = &networkingv1.IngressClass{}
	case "v1beta1":
		ic = &networkingv1beta1.IngressClass{}
	default:
		return nil, fmt.Errorf("ingressClass %s is not supported", v)
	}

	return
}

func getIngressClassSelector(sc client.ObjectList, exact []string, regex []*regexp.Regexp) (*labels.Requirement, error) {
	isIngressClassRegexed := func(name string, regex []*regexp.Regexp) bool {
		for _, r := range regex {
			if r.MatchString(name) {
				return true
			}
		}

		return false
	}

	var names []string

	switch t := sc.(type) {
	case *networkingv1beta1.IngressClassList:
		for _, i := range t.Items {
			if isIngressClassRegexed(i.GetName(), regex) {
				names = append(names, i.GetName())

				continue
			}

			if f := sort.SearchStrings(exact, i.GetName()); f < len(exact) && exact[f] == i.GetName() {
				names = append(names, i.GetName())
			}
		}
	case *networkingv1.IngressClassList:
		for _, i := range t.Items {
			if isIngressClassRegexed(i.GetName(), regex) {
				names = append(names, i.GetName())

				continue
			}

			if f := sort.SearchStrings(exact, i.GetName()); f < len(exact) && exact[f] == i.GetName() {
				names = append(names, i.GetName())
			}
		}
	}

	if len(names) > 0 {
		return labels.NewRequirement("name", selection.In, names)
	}

	return nil, fmt.Errorf("cannot create LabelSelector for the requested IngressClass requirement")
}
