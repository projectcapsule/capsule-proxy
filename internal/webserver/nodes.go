package webserver

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

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

func (n kubeFilter) nodeListHandler(writer http.ResponseWriter, request *http.Request) {
	username, _, _ := req.NewHttp(request, n.usernameClaimField).GetUserAndGroups()
	selector := n.nodeSelector(request)
	n.handleRequest(request, username, selector)
}

func (n kubeFilter) nodeGetHandler(writer http.ResponseWriter, request *http.Request) {
	selector := n.nodeSelector(request)

	nl := &corev1.NodeList{}
	if err := n.client.List(context.Background(), nl, &client.ListOptions{LabelSelector: selector}); err != nil {
		n.handleError(err, writer)
		return
	}

	if len(nl.Items) > 0 && nl.Items[0].Name == mux.Vars(request)["name"] {
		if len(n.bearerToken) > 0 {
			log.V(4).Info("Updating the token", "token", n.bearerToken)
			request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", n.bearerToken))
		}
		return
	}
	// The current user is trying to get a Node cannot access
	n.impersonateHandler(writer, request)
}

func (n kubeFilter) nodeSelector(request *http.Request) (selector labels.Selector) {
	log.V(2).Info("Decorating request for Node filtering")

	username, groups, _ := req.NewHttp(request, n.usernameClaimField).GetUserAndGroups()

	log.V(4).Info("Getting user from request", "username", username, "groups", groups)

	filter := func(tenantList *capsulev1alpha1.TenantList) *capsulev1alpha1.TenantList {
		filtered := &capsulev1alpha1.TenantList{}

		for _, tenant := range tenantList.Items {
			var annotation string
			switch request.Method {
			case "GET":
				annotation = nodeListingAnnotation
			case "PUT", "PATCH":
				annotation = nodeUpdateAnnotation
			case "DELETE":
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
				filtered.Items = append(filtered.Items, tenant)
			}
		}

		return filtered
	}

	var err error
	selector, err = n.getLabelSelectorForOwner(username, groups, filter)
	if err != nil {
		log.Error(err, "cannot create label selector")
		panic(err)
	}

	return selector
}
