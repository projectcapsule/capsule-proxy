// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"net/http"

	capsuleapi "github.com/projectcapsule/capsule/pkg/api"
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

func (o *Operations) Allow(operation capsuleapi.ProxyOperation) {
	switch operation {
	case capsuleapi.ListOperation:
		o.List = true
	case capsuleapi.UpdateOperation:
		o.Update = true
	case capsuleapi.DeleteOperation:
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
