// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"net/http"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

type Operations struct {
	List   bool
	Update bool
	Delete bool
}

func defaultOperations() *Operations {
	return &Operations{
		List:   false,
		Update: false,
		Delete: false,
	}
}

func (o *Operations) Allow(operation capsulev1beta1.ProxyOperation) {
	switch operation {
	case capsulev1beta1.ListOperation:
		o.List = true
	case capsulev1beta1.UpdateOperation:
		o.Update = true
	case capsulev1beta1.DeleteOperation:
		o.Delete = true
	}
}

func (o *Operations) IsAllowed(request *http.Request) (ok bool) {
	switch request.Method {
	case http.MethodGet:
		ok = o.List
	case http.MethodPut, http.MethodPatch:
		ok = o.List
		ok = ok && o.Update
	case http.MethodDelete:
		ok = o.List
		ok = ok && o.Delete
	default:
		break
	}

	return
}
