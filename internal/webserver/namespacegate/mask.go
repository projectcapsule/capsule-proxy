// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package namespacegate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type resourceRequest struct {
	group, namespace, resource, name string
}

func namespacedResourceRequest(path string) (resourceRequest, bool) {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if slicesContainEmpty(parts) {
		return resourceRequest{}, false
	}

	switch {
	case len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "namespaces":
		return resourceRequest{namespace: parts[3], resource: parts[4], name: parts[5]}, true
	case len(parts) == 7 && parts[0] == "apis" && parts[3] == "namespaces":
		return resourceRequest{group: parts[1], namespace: parts[4], resource: parts[5], name: parts[6]}, true
	default:
		return resourceRequest{}, false
	}
}

func slicesContainEmpty(values []string) bool {
	for _, value := range values {
		if value == "" {
			return true
		}
	}

	return false
}

func (g *Gate) maskForbiddenForMissingNamespace(response *http.Response) {
	request := response.Request
	if request.Method != http.MethodGet || request.URL == nil {
		return
	}

	watch, err := strconv.ParseBool(request.URL.Query().Get("watch"))
	if err == nil && watch {
		return
	}

	resource, ok := namespacedResourceRequest(request.URL.Path)
	if !ok {
		return
	}

	namespace := &corev1.Namespace{}
	if err := g.namespaces.Get(request.Context(), client.ObjectKey{Name: resource.namespace}, namespace); !apierrors.IsNotFound(err) {
		// The namespace either exists or its absence could not be established.
		return
	}

	status := apierrors.NewNotFound(
		schema.GroupResource{Group: resource.group, Resource: resource.resource},
		resource.name,
	).ErrStatus
	body, err := json.Marshal(status)
	if err != nil {
		return
	}

	replaceResponse(response, http.StatusNotFound, "application/json", body)
	g.log.V(4).Info(
		"masked forbidden as not found for missing namespace",
		"namespace", resource.namespace,
		"resource", resource.resource,
		"name", resource.name,
	)
}

func replaceResponse(response *http.Response, statusCode int, contentType string, body []byte) {
	if response.Body != nil {
		_ = response.Body.Close()
	}

	response.Body = io.NopCloser(bytes.NewReader(body))
	response.StatusCode = statusCode
	response.Status = fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode))
	response.ContentLength = int64(len(body))
	response.TransferEncoding = nil
	response.Uncompressed = false

	if response.Header == nil {
		response.Header = http.Header{}
	}

	response.Header.Set("Content-Length", strconv.Itoa(len(body)))
	response.Header.Set("Content-Type", contentType)
	response.Header.Del("Content-Encoding")
	response.Header.Del("Transfer-Encoding")
}
