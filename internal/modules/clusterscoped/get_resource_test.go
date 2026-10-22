// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package clusterscoped

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gorilla/mux"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/modules"
	moduleerrors "github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

type namedResourceDiscovery struct {
	discovery.DiscoveryInterface
	calls atomic.Int64
}

func (d *namedResourceDiscovery) ServerResourcesForGroupVersion(version string) (*metav1.APIResourceList, error) {
	d.calls.Add(1)
	return &metav1.APIResourceList{GroupVersion: version, APIResources: []metav1.APIResource{{Name: "persistentvolumes", Kind: "PersistentVolume"}}}, nil
}

type namedResourceReader struct {
	client.Reader
	calls   atomic.Int64
	failure error
}

func (r *namedResourceReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	r.calls.Add(1)
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.failure != nil {
		return r.failure
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}

func namedResourceFixture(t testing.TB, resolved bool, mode string) (modules.Module, *namedResourceDiscovery, *namedResourceReader, []*tenant.ProxyTenant, staticRequest) {
	t.Helper()
	gvk := schema.GroupVersionKind{Version: "v1", Kind: "PersistentVolume"}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetName(persistentVolumeName)
	obj.SetLabels(map[string]string{"selected": "true"})
	cl := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).WithObjects(obj).Build()
	r := &namedResourceReader{Reader: cl}
	d := &namedResourceDiscovery{}
	path := "/api/v1/persistentvolumes/{name}"
	var mod modules.Module = Get(d, r, cl, path)
	if resolved {
		mod = GetResource(gvk, r, cl, path)
	}
	grants := []*tenant.ProxyTenant{{ClusterResources: []v1beta1.ClusterResource{{APIGroups: []string{""}, Resources: []string{"persistentvolumes"}, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"selected": "true"}}}}}}
	name := persistentVolumeName
	method := http.MethodGet
	switch mode {
	case "excluded":
		grants[0].ClusterResources[0].Selector.MatchLabels["selected"] = "false"
	case "ungranted":
		grants = nil
	case "mutation":
		method = http.MethodDelete
	case "missing":
		name = "missing"
	case "read-error":
		r.failure = errors.New("reader unavailable")
	}
	httpRequest := mux.SetURLVars(httptest.NewRequest(method, "/api/v1/persistentvolumes/"+name, nil).WithContext(t.Context()), map[string]string{"name": name})
	if mode == "cancelled" {
		ctx, cancel := context.WithCancel(httpRequest.Context())
		cancel()
		httpRequest = httpRequest.WithContext(ctx)
	}
	return mod, d, r, grants, staticRequest{Request: httpRequest}
}

func TestGetResourceUsesStartupDiscovery(t *testing.T) {
	t.Parallel()
	for _, resolved := range []bool{false, true} {
		for _, mode := range []string{"selected", "excluded", "ungranted", "mutation", "missing", "read-error", "cancelled"} {
			t.Run(fmt.Sprintf("resolved=%t/%s", resolved, mode), func(t *testing.T) {
				mod, d, r, grants, req := namedResourceFixture(t, resolved, mode)
				selector, err := mod.Handle(grants, req)
				if (err != nil) != (mode == "missing" || mode == "read-error" || mode == "cancelled") {
					t.Fatalf("error=%v", err)
				}
				if (selector != nil) != (mode == "selected") {
					t.Fatalf("selector=%v", selector)
				}
				if mode == "missing" {
					status, ok := errors.AsType[moduleerrors.Error](err)
					if !ok || status.Status().Code != 404 {
						t.Fatalf("missing resource error=%v", err)
					}
				}
				wantReads := int64(1)
				if mode == "ungranted" || mode == "mutation" {
					wantReads = 0
				}
				wantDiscovery := wantReads
				if resolved {
					wantDiscovery = 0
				}
				if r.calls.Load() != wantReads || d.calls.Load() != wantDiscovery {
					t.Fatalf("resource GET=%d discovery=%d want=%d/%d", r.calls.Load(), d.calls.Load(), wantReads, wantDiscovery)
				}
			})
		}
	}
}

func TestGetResourceMissingKindFailsClosed(t *testing.T) {
	_, _, reader, grants, req := namedResourceFixture(t, true, "selected")
	mod := GetResource(schema.GroupVersionKind{}, reader, nil, "/api/v1/persistentvolumes/{name}")
	selector, err := mod.Handle(grants, req)
	if err == nil || selector != nil || reader.calls.Load() != 0 {
		t.Fatalf("missing kind: selector=%v error=%v reads=%d", selector, err, reader.calls.Load())
	}
}

func BenchmarkNamedResourceDiscovery(b *testing.B) {
	for _, resolved := range []bool{false, true} {
		for _, mode := range []string{"selected", "excluded", "ungranted"} {
			for _, parallel := range []bool{false, true} {
				b.Run(fmt.Sprintf("resolved=%t/%s/parallel=%t", resolved, mode, parallel), func(b *testing.B) {
					mod, d, r, grants, req := namedResourceFixture(b, resolved, mode)
					run := func() {
						_, err := mod.Handle(grants, req)
						if err != nil {
							b.Error(err)
						}
					}
					b.ReportAllocs()
					b.ResetTimer()
					if parallel {
						b.RunParallel(func(pb *testing.PB) {
							for pb.Next() {
								run()
							}
						})
					} else {
						for range b.N {
							run()
						}
					}
					b.StopTimer()
					b.ReportMetric(float64(d.calls.Load())/float64(b.N), "discovery-GET/op")
					b.ReportMetric(float64(r.calls.Load())/float64(b.N), "resource-GET/op")
				})
			}
		}
	}
}
