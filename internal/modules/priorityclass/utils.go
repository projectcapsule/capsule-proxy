package priorityclass

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	"github.com/clastix/capsule-proxy/internal/tenant"
)

func getPriorityClass(req *http.Request, proxyTenants []*tenant.ProxyTenant) (exact []string, regex []*regexp.Regexp) {
	for _, pt := range proxyTenants {
		if ok := pt.RequestAllowed(req, capsulev1beta1.PriorityClassesProxy); ok {
			pc := pt.Tenant.Spec.PriorityClasses
			if pc == nil {
				continue
			}

			if len(pc.Exact) > 0 {
				exact = append(exact, pc.Exact...)
			}

			if r := pc.Regex; len(r) > 0 {
				regex = append(regex, regexp.MustCompile(r))
			}
		}
	}

	sort.SliceStable(exact, func(i, j int) bool {
		return exact[i] < exact[0]
	})

	return exact, regex
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
