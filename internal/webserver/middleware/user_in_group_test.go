// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/sets"

	req "github.com/projectcapsule/capsule-proxy/internal/request"
)

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(http.MethodGet, "https://proxy.example/api/v1/namespaces", nil)
			request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{
				Subject: pkix.Name{CommonName: tt.username, Organization: tt.groups},
			}}}

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
			handler.ServeHTTP(httptest.NewRecorder(), request)

			if bypassed != tt.wantBypass {
				t.Fatalf("bypass called = %v, want %v", bypassed, tt.wantBypass)
			}
			if continued == tt.wantBypass {
				t.Fatalf("next handler called = %v, want %v", continued, !tt.wantBypass)
			}
		})
	}
}
