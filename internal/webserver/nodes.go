package webserver

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/selection"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"github.com/gorilla/mux"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	req "github.com/clastix/capsule-proxy/internal/request"
)

const (
	nodeListingAnnotation  = "capsule.clastix.io/enable-node-listing"
	nodeUpdateAnnotation   = "capsule.clastix.io/enable-node-update"
	nodeDeletionAnnotation = "capsule.clastix.io/enable-node-deletion"
)

func (n kubeFilter) registerNode(router *mux.Router) {
	node := router.PathPrefix("/api/v1/nodes").Subrouter()
	node.Use(n.checkJWTMiddleware, n.checkUserInCapsuleGroupMiddleware)
	node.HandleFunc("", func(writer http.ResponseWriter, request *http.Request) {
		n.nodeListHandler(writer, request)
	})
	node.HandleFunc("/{name}", func(writer http.ResponseWriter, request *http.Request) {
		n.nodeGetHandler(writer, request)
	})
}

func (n kubeFilter) getNodeSelector(nl *corev1.NodeList, selectors []map[string]string) (*labels.Requirement, error) {
	var names []string
	for _, node := range nl.Items {
		for _, selector := range selectors {
			matches := 0
			for k := range selector {
				if selector[k] == node.GetLabels()[k] {
					matches++
				}
			}
			if matches == len(selector) {
				names = append(names, node.GetName())
			}
		}
	}
	if len(names) > 0 {
		return labels.NewRequirement("kubernetes.io/hostname", selection.In, names)
	}
	return nil, fmt.Errorf("cannot create LabelSelector for the requested Node requirement")
}

func (n kubeFilter) getNodeSelectors(request *http.Request, tenantList *capsulev1alpha1.TenantList) (selectors []map[string]string) {
	for _, tenant := range tenantList.Items {
		var annotation string
		switch request.Method {
		case http.MethodGet:
			annotation = nodeListingAnnotation
		case http.MethodPut, http.MethodPatch:
			annotation = nodeUpdateAnnotation
		case http.MethodDelete:
			annotation = nodeDeletionAnnotation
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
			selectors = append(selectors, tenant.Spec.NodeSelector)
		}
	}
	return
}

func (n kubeFilter) nodeGetHandler(w http.ResponseWriter, request *http.Request) {
	username, groups, _ := req.NewHTTP(request, n.usernameClaimField, n.client).GetUserAndGroups()
	tenantList, err := n.getTenantsForOwner(username, groups)
	if err != nil {
		handleError(w, err, "cannot list Tenant resources")
	}

	selectors := n.getNodeSelectors(request, tenantList)

	nl := &corev1.NodeList{}
	if err = n.client.List(context.Background(), nl, client.MatchingLabels{"kubernetes.io/hostname": mux.Vars(request)["name"]}); err != nil {
		handleError(w, err, "cannot list Node resources")
	}

	var r *labels.Requirement
	r, err = n.getNodeSelector(nl, selectors)
	if err == nil {
		n.handleRequest(request, username, labels.NewSelector().Add(*r))
		return
	}

	switch request.Method {
	case http.MethodGet:
		handleNotFound(
			w,
			fmt.Sprintf("nodes \"%s\" not found", mux.Vars(request)["name"]),
			&metav1.StatusDetails{
				Name: mux.Vars(request)["name"],
				Kind: "nodes",
			},
		)
	default:
		n.impersonateHandler(w, request)
	}
}

func (n kubeFilter) nodeListHandler(w http.ResponseWriter, request *http.Request) {
	username, groups, _ := req.NewHTTP(request, n.usernameClaimField, n.client).GetUserAndGroups()
	tenantList, err := n.getTenantsForOwner(username, groups)
	if err != nil {
		handleError(w, err, "cannot list Tenant resources")
	}

	selectors := n.getNodeSelectors(request, tenantList)

	nl := &corev1.NodeList{}
	if err = n.client.List(context.Background(), nl); err != nil {
		handleError(w, err, "cannot list Node resources")
	}

	var r *labels.Requirement
	r, err = n.getNodeSelector(nl, selectors)
	if err != nil {
		r, _ = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	n.handleRequest(request, username, labels.NewSelector().Add(*r))
}
