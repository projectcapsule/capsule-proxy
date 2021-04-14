package node

import (
	"fmt"
	"net/http"
	"strconv"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
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

		if ok, _ = strconv.ParseBool(strVal); ok {
			selectors = append(selectors, tenant.Spec.NodeSelector)
		}
	}

	return
}
