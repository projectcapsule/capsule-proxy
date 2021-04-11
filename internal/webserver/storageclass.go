package webserver

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"

	"github.com/gorilla/mux"
	v1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"

	req "github.com/clastix/capsule-proxy/internal/request"
)

const (
	storageClassListingAnnotation  = "capsule.clastix.io/enable-storageclass-listing"
	storageClassUpdateAnnotation   = "capsule.clastix.io/enable-storageclass-update"
	storageClassDeletionAnnotation = "capsule.clastix.io/enable-storageclass-deletion"
)

func (n kubeFilter) registerStorageClass(router *mux.Router) {
	sc := router.PathPrefix("/apis/storage.k8s.io/v1/storageclasses").Subrouter()
	sc.Use(n.checkJWTMiddleware, n.checkUserInCapsuleGroupMiddleware)
	sc.HandleFunc("", func(writer http.ResponseWriter, request *http.Request) {
		n.storageClassListHandler(writer, request)
	})
	sc.HandleFunc("/{name}", func(writer http.ResponseWriter, request *http.Request) {
		n.storageClassGetHandler(writer, request)
	})
}

func (n kubeFilter) getStorageClasses(request *http.Request, tenantList *capsulev1alpha1.TenantList) (exact []string, regex []*regexp.Regexp) {
	for _, tenant := range tenantList.Items {
		var annotation string
		switch request.Method {
		case http.MethodGet:
			annotation = storageClassListingAnnotation
		case http.MethodPut, http.MethodPatch:
			annotation = storageClassUpdateAnnotation
		case http.MethodDelete:
			annotation = storageClassDeletionAnnotation
		default:
			break
		}

		var ok bool
		var strVal string
		strVal, ok = tenant.Annotations[annotation]
		if !ok {
			continue
		}

		var err error
		ok, err = strconv.ParseBool(strVal)
		if err != nil {
			log.Error(err, "unable to parse value for tenant annotation", "tenant", tenant.GetName(), "annotation", annotation, "value", strVal)
			continue
		}

		if ok {
			sc := tenant.Spec.StorageClasses
			if sc == nil {
				continue
			}
			if len(sc.Exact) > 0 {
				exact = append(exact, sc.Exact...)
			}
			if r := sc.Regex; len(r) > 0 {
				regex = append(regex, regexp.MustCompile(r))
			}
		}
	}

	sort.SliceStable(exact, func(i, j int) bool {
		return exact[i] < exact[0]
	})

	return
}

func (n kubeFilter) getStorageClassSelector(sc *v1.StorageClassList, exact []string, regex []*regexp.Regexp) (*labels.Requirement, error) {
	isStorageClassRegexed := func(name string, regex []*regexp.Regexp) bool {
		for _, r := range regex {
			if r.MatchString(name) {
				return true
			}
		}
		return false
	}

	var names []string
	for _, s := range sc.Items {
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

func (n kubeFilter) storageClassGetHandler(w http.ResponseWriter, request *http.Request) {
	username, groups, _ := req.NewHTTP(request, n.usernameClaimField, n.client).GetUserAndGroups()
	tenantList, err := n.getTenantsForOwner(username, groups)
	if err != nil {
		handleError(w, err, "cannot list Tenant resources")
	}

	exactMatch, regexMatch := n.getStorageClasses(request, tenantList)

	sc := &v1.StorageClassList{}
	if err = n.client.List(context.Background(), sc, client.MatchingLabels{"name": mux.Vars(request)["name"]}); err != nil {
		handleError(w, err, "cannot list StorageClass resources")
	}

	var r *labels.Requirement
	r, err = n.getStorageClassSelector(sc, exactMatch, regexMatch)
	if err == nil {
		n.handleRequest(request, username, labels.NewSelector().Add(*r))
		return
	}

	switch request.Method {
	case http.MethodGet:
		handleNotFound(
			w,
			fmt.Sprintf("storageclasses.storage.k8s.io \"%s\" not found", mux.Vars(request)["name"]),
			&metav1.StatusDetails{
				Name:  mux.Vars(request)["name"],
				Group: "storage.k8s.io",
				Kind:  "storageclasses",
			},
		)
	default:
		n.impersonateHandler(w, request)
	}
}

func (n kubeFilter) storageClassListHandler(w http.ResponseWriter, request *http.Request) {
	username, groups, _ := req.NewHTTP(request, n.usernameClaimField, n.client).GetUserAndGroups()
	tenantList, err := n.getTenantsForOwner(username, groups)
	if err != nil {
		handleError(w, err, "cannot list Tenant resources")
	}

	exactMatch, regexMatch := n.getStorageClasses(request, tenantList)

	sc := &v1.StorageClassList{}
	if err = n.client.List(context.Background(), sc); err != nil {
		handleError(w, err, "cannot list StorageClass resources")
	}

	var r *labels.Requirement
	r, err = n.getStorageClassSelector(sc, exactMatch, regexMatch)
	if err != nil {
		r, _ = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	n.handleRequest(request, username, labels.NewSelector().Add(*r))
}
