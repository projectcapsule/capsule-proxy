package ingressclass

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"github.com/gorilla/mux"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ingressClassListingAnnotation  = "capsule.clastix.io/enable-ingressclass-listing"
	ingressClassUpdateAnnotation   = "capsule.clastix.io/enable-ingressclass-update"
	ingressClassDeletionAnnotation = "capsule.clastix.io/enable-ingressclass-deletion"
)

func getIngressClasses(request *http.Request, tenantList *capsulev1alpha1.TenantList) (exact []string, regex []*regexp.Regexp) {
	for _, tenant := range tenantList.Items {
		var annotation string

		switch request.Method {
		case http.MethodGet:
			annotation = ingressClassListingAnnotation
		case http.MethodPut, http.MethodPatch:
			annotation = ingressClassUpdateAnnotation
		case http.MethodDelete:
			annotation = ingressClassDeletionAnnotation
		default:
			break
		}

		var ok bool

		var strVal string

		strVal, ok = tenant.Annotations[annotation]
		if !ok {
			continue
		}

		ok, _ = strconv.ParseBool(strVal)

		if ok {
			ic := tenant.Spec.IngressClasses
			if ic == nil {
				continue
			}

			if len(ic.Exact) > 0 {
				exact = append(exact, ic.Exact...)
			}

			if r := ic.Regex; len(r) > 0 {
				regex = append(regex, regexp.MustCompile(r))
			}
		}
	}

	sort.SliceStable(exact, func(i, j int) bool {
		return exact[i] < exact[0]
	})

	return exact, regex
}

func getIngressClassFromRequest(request *http.Request) (ic client.ObjectList, err error) {
	v := mux.Vars(request)["version"]
	switch v {
	case "v1":
		ic = &networkingv1.IngressClassList{}
	case "v1beta1":
		ic = &networkingv1beta1.IngressClassList{}
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
