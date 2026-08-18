// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package namespacegate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNamespacedResourceRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want resourceRequest
		ok   bool
	}{
		{
			name: "core resource",
			path: "/api/v1/namespaces/tenant-a/services/app",
			want: resourceRequest{namespace: "tenant-a", resource: "services", name: "app"},
			ok:   true,
		},
		{
			name: "grouped resource",
			path: "/apis/apps/v1/namespaces/tenant-a/deployments/app",
			want: resourceRequest{group: "apps", namespace: "tenant-a", resource: "deployments", name: "app"},
			ok:   true,
		},
		{name: "collection", path: "/api/v1/namespaces/tenant-a/services"},
		{name: "subresource", path: "/api/v1/namespaces/tenant-a/pods/app/log"},
		{name: "missing name", path: "/api/v1/namespaces/tenant-a/services/"},
		{name: "empty path segment", path: "/api/v1/namespaces//services/app"},
		{name: "cluster scoped", path: "/apis/rbac.authorization.k8s.io/v1/clusterroles/app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := namespacedResourceRequest(tt.path)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("namespacedResourceRequest(%q) = (%+v, %v), want (%+v, %v)", tt.path, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestMaskForbiddenOnlyWhenNamespaceIsMissing(t *testing.T) {
	t.Parallel()

	lookupFailure := errors.New("namespace lookup failed")
	tests := []struct {
		name           string
		method         string
		path           string
		upstreamStatus int
		namespace      *corev1.Namespace
		lookupError    error
		wantStatus     int
		wantGets       int
		wantGroup      string
		wantResource   string
	}{
		{
			name:           "missing namespace for core resource",
			method:         http.MethodGet,
			path:           "/api/v1/namespaces/acme-app/serviceaccounts/release",
			upstreamStatus: http.StatusForbidden,
			wantStatus:     http.StatusNotFound,
			wantGets:       1,
			wantResource:   "serviceaccounts",
		},
		{
			name:           "missing namespace for grouped resource",
			method:         http.MethodGet,
			path:           "/apis/apps/v1/namespaces/acme-app/deployments/release",
			upstreamStatus: http.StatusForbidden,
			wantStatus:     http.StatusNotFound,
			wantGets:       1,
			wantGroup:      "apps",
			wantResource:   "deployments",
		},
		{
			name:           "existing namespace",
			method:         http.MethodGet,
			path:           "/api/v1/namespaces/acme-app/serviceaccounts/release",
			upstreamStatus: http.StatusForbidden,
			namespace:      &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "acme-app"}},
			wantStatus:     http.StatusForbidden,
			wantGets:       1,
		},
		{
			name:           "namespace lookup error fails closed",
			method:         http.MethodGet,
			path:           "/api/v1/namespaces/acme-app/serviceaccounts/release",
			upstreamStatus: http.StatusForbidden,
			lookupError:    lookupFailure,
			wantStatus:     http.StatusForbidden,
			wantGets:       1,
		},
		{
			name:           "non forbidden upstream response",
			method:         http.MethodGet,
			path:           "/api/v1/namespaces/acme-app/serviceaccounts/release",
			upstreamStatus: http.StatusUnauthorized,
			wantStatus:     http.StatusUnauthorized,
		},
		{
			name:           "non get request",
			method:         http.MethodDelete,
			path:           "/api/v1/namespaces/acme-app/serviceaccounts/release",
			upstreamStatus: http.StatusForbidden,
			wantStatus:     http.StatusForbidden,
		},
		{
			name:           "collection request",
			method:         http.MethodGet,
			path:           "/api/v1/namespaces/acme-app/serviceaccounts",
			upstreamStatus: http.StatusForbidden,
			wantStatus:     http.StatusForbidden,
		},
		{
			name:           "subresource request",
			method:         http.MethodGet,
			path:           "/api/v1/namespaces/acme-app/pods/release/log",
			upstreamStatus: http.StatusForbidden,
			wantStatus:     http.StatusForbidden,
		},
		{
			name:           "watch request",
			method:         http.MethodGet,
			path:           "/api/v1/namespaces/acme-app/pods/release?watch=true",
			upstreamStatus: http.StatusForbidden,
			wantStatus:     http.StatusForbidden,
		},
		{
			name:           "explicit non watch request",
			method:         http.MethodGet,
			path:           "/api/v1/namespaces/acme-app/pods/release?watch=false",
			upstreamStatus: http.StatusForbidden,
			wantStatus:     http.StatusNotFound,
			wantGets:       1,
			wantResource:   "pods",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var objects []client.Object
			if tt.namespace != nil {
				objects = append(objects, tt.namespace)
			}
			reader := newTrackingReader(t, tt.lookupError, objects...)
			gate := New(reader, logr.Discard())
			request := httptest.NewRequest(tt.method, "https://proxy.example"+tt.path, nil)
			originalBody := []byte(`{"kind":"Status","code":403}`)
			response := &http.Response{
				StatusCode: tt.upstreamStatus,
				Status:     strconv.Itoa(tt.upstreamStatus),
				Request:    request,
				Header:     http.Header{"Content-Type": {"application/json"}, "Content-Encoding": {"gzip"}},
				Body:       io.NopCloser(bytes.NewReader(originalBody)),
			}

			if err := gate.ModifyResponse(response); err != nil {
				t.Fatal(err)
			}

			if response.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.StatusCode, tt.wantStatus)
			}
			if reader.gets != tt.wantGets {
				t.Fatalf("namespace GETs = %d, want %d", reader.gets, tt.wantGets)
			}

			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}

			if tt.wantStatus != http.StatusNotFound {
				if !bytes.Equal(body, originalBody) {
					t.Fatalf("upstream body changed: %s", body)
				}

				return
			}

			status := &metav1.Status{}
			if err := json.Unmarshal(body, status); err != nil {
				t.Fatal(err)
			}
			if !apierrors.IsNotFound(&apierrors.StatusError{ErrStatus: *status}) {
				t.Fatalf("rewritten response is not a Kubernetes NotFound status: %s", body)
			}
			if status.Details == nil || status.Details.Group != tt.wantGroup || status.Details.Kind != tt.wantResource {
				t.Fatalf("unexpected status details: %+v", status.Details)
			}
			if response.Header.Get("Content-Type") != "application/json" || response.Header.Get("Content-Encoding") != "" {
				t.Fatalf("unexpected response headers: %v", response.Header)
			}
			if response.Header.Get("Content-Length") != strconv.Itoa(len(body)) || response.ContentLength != int64(len(body)) {
				t.Fatalf("incorrect content length for %d-byte body", len(body))
			}
		})
	}
}

func TestModifyResponseHandlesMissingContext(t *testing.T) {
	t.Parallel()

	gate := New(newTrackingReader(t, nil), logr.Discard())
	if err := gate.ModifyResponse(nil); err != nil {
		t.Fatal(err)
	}
	if err := gate.ModifyResponse(&http.Response{StatusCode: http.StatusForbidden}); err != nil {
		t.Fatal(err)
	}
}

type trackingReader struct {
	client.Reader

	err  error
	gets int
}

func newTrackingReader(t *testing.T, getError error, objects ...client.Object) *trackingReader {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	return &trackingReader{
		Reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build(),
		err:    getError,
	}
}

func (r *trackingReader) Get(ctx context.Context, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
	r.gets++
	if r.err != nil {
		return r.err
	}

	return r.Reader.Get(ctx, key, object, opts...)
}
