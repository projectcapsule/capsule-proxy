package node

import (
	"fmt"
	"net/http"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	utils "github.com/clastix/capsule-proxy/internal/modules/utils"
)

const (
	nodeListingAnnotation  = "capsule.clastix.io/enable-node-listing"
	nodeUpdateAnnotation   = "capsule.clastix.io/enable-node-update"
	nodeDeletionAnnotation = "capsule.clastix.io/enable-node-deletion"
)

func getNodeSelector(nl *corev1.NodeList, selectors []map[string]string) (*labels.Requirement, error) {
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

func getNodeSelectors(request *http.Request, tenantList *capsulev1alpha1.TenantList) (selectors []map[string]string) {
	for _, tenant := range tenantList.Items {
		var ok bool

		switch request.Method {
		case http.MethodGet:
			ok = utils.IsAnnotationTrue(tenant, nodeListingAnnotation)
		case http.MethodPut, http.MethodPatch:
			ok = utils.IsAnnotationTrue(tenant, nodeListingAnnotation)
			ok = ok && utils.IsAnnotationTrue(tenant, nodeUpdateAnnotation)
		case http.MethodDelete:
			ok = utils.IsAnnotationTrue(tenant, nodeListingAnnotation)
			ok = ok && utils.IsAnnotationTrue(tenant, nodeDeletionAnnotation)
		default:
			break
		}

		if ok {
			selectors = append(selectors, tenant.Spec.NodeSelector)
		}
	}

	return
}
