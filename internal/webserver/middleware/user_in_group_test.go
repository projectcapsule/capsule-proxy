// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	req "github.com/projectcapsule/capsule-proxy/internal/request"
)

func TestHandleResolveUserAndGroupsError(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		err    error
		code   int
		reason metav1.StatusReason
	}{
		{"unauthorized", req.NewErrUnauthorized("denied"), http.StatusForbidden, metav1.StatusReasonForbidden},
		{"wrapped unauthorized", fmt.Errorf("identity: %w", req.NewErrUnauthorized("denied")), http.StatusForbidden, metav1.StatusReasonForbidden},
		{"joined unauthorized", errors.Join(errors.New("review"), req.NewErrUnauthorized("denied")), http.StatusForbidden, metav1.StatusReasonForbidden},
		{"internal error", errors.New("review unavailable"), http.StatusInternalServerError, metav1.StatusReasonInternalError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			handleResolveUserAndGroupsError(response, tt.err)
			var status metav1.Status
			if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
				t.Fatal(err)
			}
			if response.Code != tt.code || status.Code != int32(tt.code) || status.Reason != tt.reason {
				t.Fatalf("response = %d, %+v; want %d, %s", response.Code, status, tt.code, tt.reason)
			}
			if status.Kind != "Status" || status.APIVersion != "v1" || !strings.Contains(status.Message, tt.err.Error()) {
				t.Fatalf("unexpected Kubernetes Status: %+v", status)
			}
		})
	}
}

func BenchmarkHandleResolveUserAndGroupsError(b *testing.B) {
	for _, tt := range []struct {
		name string
		err  error
		code int
	}{
		{"unauthorized", req.NewErrUnauthorized("denied"), http.StatusForbidden},
		{"wrapped", fmt.Errorf("identity: %w", req.NewErrUnauthorized("denied")), http.StatusForbidden},
		{"internal", errors.New("review unavailable"), http.StatusInternalServerError},
	} {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				response := httptest.NewRecorder()
				handleResolveUserAndGroupsError(response, tt.err)
				if response.Code != tt.code {
					b.Fatalf("response code = %d, want %d", response.Code, tt.code)
				}
			}
		})
	}
}

func TestIdentityIsIgnored(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		username         string
		groups           []string
		ignoredUsernames sets.Set[string]
		ignoredGroups    sets.Set[string]
		want             bool
	}{
		{
			name:             "username",
			username:         "alice",
			ignoredUsernames: sets.New("alice"),
			ignoredGroups:    sets.New[string](),
			want:             true,
		},
		{
			name:             "group",
			username:         "alice",
			groups:           []string{"developers", "platform"},
			ignoredUsernames: sets.New[string](),
			ignoredGroups:    sets.New("platform"),
			want:             true,
		},
		{
			name:             "not ignored",
			username:         "alice",
			groups:           []string{"developers"},
			ignoredUsernames: sets.New("bob"),
			ignoredGroups:    sets.New("platform"),
		},
		{
			name:             "username matching is case sensitive",
			username:         "alice",
			ignoredUsernames: sets.New("Alice"),
			ignoredGroups:    sets.New[string](),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := identityIsIgnored(tt.username, tt.groups, tt.ignoredUsernames, tt.ignoredGroups); got != tt.want {
				t.Fatalf("identityIsIgnored() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckUserInIgnoredIdentityMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		username         string
		groups           []string
		ignoredUsernames sets.Set[string]
		ignoredGroups    sets.Set[string]
		wantBypass       bool
		wantDenied       bool
	}{
		{
			name:             "ignored username bypasses filtering",
			username:         "alice",
			groups:           []string{"developers"},
			ignoredUsernames: sets.New("alice"),
			ignoredGroups:    sets.New[string](),
			wantBypass:       true,
		},
		{
			name:             "ignored group bypasses filtering",
			username:         "alice",
			groups:           []string{"platform"},
			ignoredUsernames: sets.New[string](),
			ignoredGroups:    sets.New("platform"),
			wantBypass:       true,
		},
		{
			name:             "regular identity continues filtering",
			username:         "alice",
			groups:           []string{"developers"},
			ignoredUsernames: sets.New("bob"),
			ignoredGroups:    sets.New("platform"),
		},
		{
			name:             "missing identity is rejected before either handler",
			ignoredUsernames: sets.New("bob"),
			ignoredGroups:    sets.New("platform"),
			wantDenied:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(http.MethodGet, "https://proxy.example/api/v1/namespaces", nil)
			request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{
				Subject: pkix.Name{CommonName: tt.username, Organization: tt.groups},
			}}}
			if tt.wantDenied {
				request.TLS = nil
			}

			bypassed := false
			continued := false
			middleware := CheckUserInIgnoredIdentityMiddleware(
				nil,
				logr.Discard(),
				"preferred_username",
				[]req.AuthType{req.TLSCertificate},
				tt.ignoredUsernames,
				tt.ignoredGroups,
				nil,
				nil,
				false,
				"X-Forwarded-Client-Cert",
				func(http.ResponseWriter, *http.Request) { bypassed = true },
			)
			handler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { continued = true }))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if tt.wantDenied {
				if response.Code != http.StatusForbidden || bypassed || continued {
					t.Fatalf("unauthenticated request: status=%d, bypass=%t, continued=%t", response.Code, bypassed, continued)
				}
				return
			}

			if bypassed != tt.wantBypass {
				t.Fatalf("bypass called = %v, want %v", bypassed, tt.wantBypass)
			}
			if continued == tt.wantBypass {
				t.Fatalf("next handler called = %v, want %v", continued, !tt.wantBypass)
			}
		})
	}
}
