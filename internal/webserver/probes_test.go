// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type probeServerOptions struct {
	tls  bool
	port uint
}

func (o probeServerOptions) IsListeningTLS() bool                        { return o.tls }
func (o probeServerOptions) ListeningPort() uint                         { return o.port }
func (o probeServerOptions) TLSCertificatePath() string                  { return "" }
func (o probeServerOptions) TLSCertificateKeyPath() string               { return "" }
func (o probeServerOptions) GetCertificateAuthorityPool() *x509.CertPool { return nil }

func probeOptions(t testing.TB, server *httptest.Server) probeServerOptions {
	t.Helper()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil {
		t.Fatal(err)
	}

	return probeServerOptions{tls: u.Scheme == "https", port: uint(port)}
}

func TestReadinessProbeReusesConnections(t *testing.T) {
	t.Parallel()

	for _, tls := range []bool{false, true} {
		name := "HTTP"
		if tls {
			name = "HTTPS"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var connections, requests atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/_healthz" {
					t.Errorf("probe request = %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "" || r.Header.Get("Impersonate-User") != "" {
					t.Error("probe forwarded caller credentials")
				}
				_, _ = w.Write([]byte("ok"))
			}))
			server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					connections.Add(1)
				}
			}
			if tls {
				server.StartTLS()
			} else {
				server.Start()
			}
			t.Cleanup(server.Close)

			filter := &kubeFilter{serverOptions: probeOptions(t, server), readinessClient: newReadinessClient()}
			t.Cleanup(filter.readinessClient.CloseIdleConnections)
			request := httptest.NewRequest(http.MethodGet, "/readyz/", nil)
			request.Header.Set("Authorization", "Bearer caller-token")
			request.Header.Set("Impersonate-User", "alice")

			for range 200 {
				ctx, cancel := context.WithCancel(request.Context())
				err := filter.ReadinessProbe(request.WithContext(ctx))
				cancel()
				if err != nil {
					t.Fatal(err)
				}
			}

			if got := connections.Load(); got != 1 {
				t.Fatalf("connections after 200 probes = %d, want 1", got)
			}
			if got := requests.Load(); got != 200 {
				t.Fatalf("health requests = %d, want 200", got)
			}

			server.Close()
			if err := filter.ReadinessProbe(request); err == nil {
				t.Fatal("probe succeeded after the listener stopped")
			}
		})
	}
}

func TestReadinessProbeConcurrentConnections(t *testing.T) {
	t.Parallel()

	var connections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	filter := &kubeFilter{serverOptions: probeOptions(t, server), readinessClient: newReadinessClient()}
	t.Cleanup(filter.readinessClient.CloseIdleConnections)
	request := httptest.NewRequest(http.MethodGet, "/readyz/", nil)
	errors := make(chan error, 200)
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			for range 10 {
				errors <- filter.ReadinessProbe(request)
			}
		})
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := connections.Load(); got > 2 {
		t.Fatalf("connections after 200 concurrent probes = %d, want at most 2", got)
	}
}

type probeBody struct {
	io.Reader
	closed bool
}

func (b *probeBody) Close() error {
	b.closed = true

	return nil
}

type probeErrorReader struct{ err error }

func (r probeErrorReader) Read([]byte) (int, error) { return 0, r.err }

func TestReadinessProbeResponses(t *testing.T) {
	t.Parallel()

	readError := errors.New("health response read failed")
	for _, tt := range []struct {
		name      string
		status    int
		reader    io.Reader
		wantError string
		wantCause error
	}{
		{name: "healthy", status: http.StatusOK, reader: strings.NewReader("ok")},
		{name: "empty healthy response", status: http.StatusOK, reader: strings.NewReader("")},
		{name: "unhealthy", status: http.StatusServiceUnavailable, reader: strings.NewReader("unhealthy"), wantError: "503, expected 200"},
		{name: "body read error", status: http.StatusOK, reader: probeErrorReader{err: readError}, wantError: "cannot read local _healthz response", wantCause: readError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := &probeBody{Reader: tt.reader}
			filter := &kubeFilter{
				serverOptions: probeServerOptions{},
				readinessClient: &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: tt.status, Body: body, Header: make(http.Header), Request: r}, nil
				})},
			}
			err := filter.ReadinessProbe(httptest.NewRequest(http.MethodGet, "/readyz/", nil))
			if tt.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
				if reader := tt.reader.(*strings.Reader); reader.Len() != 0 {
					t.Fatal("successful health response was not drained")
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("probe error = %v, want %q", err, tt.wantError)
			}
			if tt.wantCause != nil && !errors.Is(err, tt.wantCause) {
				t.Fatalf("probe error = %v, want cause %v", err, tt.wantCause)
			}
			if !body.closed {
				t.Fatal("health response body was not closed")
			}
		})
	}
}

func TestReadinessProbeCancellationAndTimeout(t *testing.T) {
	t.Parallel()

	for _, headersSent := range []bool{false, true} {
		name := "headers"
		if headersSent {
			name = "body"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if headersSent {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			t.Cleanup(server.Close)
			filter := &kubeFilter{serverOptions: probeOptions(t, server), readinessClient: newReadinessClient()}
			t.Cleanup(filter.readinessClient.CloseIdleConnections)
			filter.readinessClient.Timeout = 50 * time.Millisecond
			request := httptest.NewRequest(http.MethodGet, "/readyz/", nil)

			if err := filter.ReadinessProbe(request); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("stalled probe error = %v, want deadline exceeded", err)
			}

			ctx, cancel := context.WithCancel(request.Context())
			cancel()
			if err := filter.ReadinessProbe(request.WithContext(ctx)); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled probe error = %v, want context canceled", err)
			}
		})
	}
}

func TestLivenessProbeDoesNotMakeRequests(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	filter := &kubeFilter{readinessClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)

		return nil, errors.New("liveness must not make HTTP requests")
	})}}
	request := httptest.NewRequest(http.MethodGet, "/healthz/", nil)
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	for range 200 {
		if err := filter.LivenessProbe(request.WithContext(ctx)); err != nil {
			t.Fatal(err)
		}
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("liveness HTTP requests = %d, want 0", got)
	}
}

func BenchmarkReadinessProbe(b *testing.B) {
	for _, tls := range []bool{false, true} {
		name := "HTTP"
		if tls {
			name = "HTTPS"
		}

		for _, parallel := range []bool{false, true} {
			mode := "Sequential"
			if parallel {
				mode = "Concurrent"
			}

			b.Run(name+"/"+mode, func(b *testing.B) {
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte("ok"))
				}))
				if tls {
					server.StartTLS()
				} else {
					server.Start()
				}
				b.Cleanup(server.Close)

				filter := &kubeFilter{serverOptions: probeOptions(b, server), readinessClient: newReadinessClient()}
				b.Cleanup(filter.readinessClient.CloseIdleConnections)
				request := httptest.NewRequest(http.MethodGet, "/readyz/", nil)
				b.ReportAllocs()
				b.ResetTimer()

				if parallel {
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							if err := filter.ReadinessProbe(request); err != nil {
								b.Error(err)
							}
						}
					})
				} else {
					for range b.N {
						if err := filter.ReadinessProbe(request); err != nil {
							b.Fatal(err)
						}
					}
				}
			})
		}
	}
}
