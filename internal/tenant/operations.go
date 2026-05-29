// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"net/http"

	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
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

func (o *Operations) Allow(operation capsulerbac.ProxyOperation) {
	switch operation {
	case capsulerbac.ListOperation:
		o.List = true
	case capsulerbac.UpdateOperation:
		o.Update = true
	case capsulerbac.DeleteOperation:
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
