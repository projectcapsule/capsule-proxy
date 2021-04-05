package webserver

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"github.com/gorilla/mux"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"

	req "github.com/clastix/capsule-proxy/internal/request"
)

func (n kubeFilter) registerIngressClass(router *mux.Router) {
	ic := router.PathPrefix("/apis/networking.k8s.io/{version}/ingressclasses").Subrouter()
	ic.Use(n.checkJWTMiddleware, n.checkUserInCapsuleGroupMiddleware)
	ic.HandleFunc("", func(writer http.ResponseWriter, request *http.Request) {
		n.ingressClassHandler(writer, request)
	})
	ic.HandleFunc("/{name}", func(writer http.ResponseWriter, request *http.Request) {
		n.ingressClassHandler(writer, request)
	})
}

func (n kubeFilter) getIngressClasses(tenantList *capsulev1alpha1.TenantList) (exact []string, regex []*regexp.Regexp) {
	for _, tnt := range tenantList.Items {
		ic := tnt.Spec.IngressClasses
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

	sort.SliceStable(exact, func(i, j int) bool {
		return exact[i] < exact[0]
	})

	return
}

func (n kubeFilter) getIngressClassSelector(sc client.ObjectList, exact []string, regex []*regexp.Regexp) (*labels.Requirement, error) {
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
	return labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
}

func (n kubeFilter) ingressClassHandler(w http.ResponseWriter, request *http.Request) {
	username, groups, _ := req.NewHTTP(request, n.usernameClaimField).GetUserAndGroups()
	tenantList, err := n.getTenantsForOwner(username, groups)
	if err != nil {
		handleError(w, err, "cannot list Tenant resources")
	}

	path, err := mux.CurrentRoute(request).GetPathTemplate()
	if err != nil {
		handleError(w, err, "unable to get request path template")
	}

	exactMatch, regexMatch := n.getIngressClasses(tenantList)

	var ic client.ObjectList
	v := mux.Vars(request)["version"]
	switch v {
	case "v1":
		ic = &networkingv1.IngressClassList{}
	case "v1beta1":
		ic = &networkingv1beta1.IngressClassList{}
	default:
		handleError(w, fmt.Errorf("ingressClass %s is not supported", v), "cannot list IngressClass")
	}

	switch path {
	case "/apis/networking.k8s.io/{version}/ingressclasses":
		if err = n.client.List(context.Background(), ic); err != nil {
			handleError(w, err, "cannot list IngressClass resources")
		}
	case "/apis/networking.k8s.io/{version}/ingressclasses/{name}":
		if err = n.client.List(context.Background(), ic, client.MatchingLabels{"name": mux.Vars(request)["name"]}); err != nil {
			handleError(w, err, "cannot list IngressClass resource")
		}
	default:
		handleError(w, fmt.Errorf("%s path not found", path), "cannot list IngressClass resource")
	}

	var r *labels.Requirement
	r, err = n.getIngressClassSelector(ic, exactMatch, regexMatch)
	if err != nil {
		handleError(w, err, "cannot create LabelSelector for the requested IngressClass requirement")
	}

	n.handleRequest(request, username, labels.NewSelector().Add(*r))
}
