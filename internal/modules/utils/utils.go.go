package options

import (
	"strconv"

	"github.com/clastix/capsule/api/v1alpha1"
)

func IsAnnotationTrue(tenant v1alpha1.Tenant, annotation string) (ok bool) {
	var value string

	value, ok = tenant.Annotations[annotation]
	if !ok {
		return
	}

	ok, _ = strconv.ParseBool(value)

	return
}
