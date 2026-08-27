// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	"github.com/golang-jwt/jwt/v5"
	"k8s.io/apimachinery/pkg/labels"
)

func TestBearerExpirationTime(t *testing.T) {
	t.Parallel()

	expiresAt := time.Now().Add(time.Hour).Truncate(time.Second)
	validToken, err := jwt.NewWithClaims(
		jwt.SigningMethodHS256,
		jwt.MapClaims{"exp": expiresAt.Unix()},
	).SignedString([]byte("test-key"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		token string
		want  time.Time
	}{
		{name: "empty token"},
		{name: "malformed token", token: "not-a-jwt"},
		{name: "valid expiration", token: validToken, want: expiresAt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := bearerExpirationTime(tt.token); !got.Equal(tt.want) {
				t.Fatalf("bearerExpirationTime() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBearerTokenWithoutTokenFile(t *testing.T) {
	t.Parallel()

	const configuredToken = "configured-token"

	filter := &kubeFilter{
		bearerToken:               configuredToken,
		bearerTokenExpirationTime: time.Time{},
	}

	if got := filter.BearerToken(); got != configuredToken {
		t.Fatalf("BearerToken() = %q, want %q", got, configuredToken)
	}
}

func TestBearerTokenReload(t *testing.T) {
	t.Parallel()

	const configuredToken = "configured-token"

	tokenFile := filepath.Join(t.TempDir(), "token")
	var logs strings.Builder

	filter := &kubeFilter{
		bearerToken:               configuredToken,
		bearerTokenFile:           tokenFile,
		bearerTokenExpirationTime: time.Time{},
		log: funcr.New(func(_, args string) {
			logs.WriteString(args)
		}, funcr.Options{Verbosity: 10}),
	}

	if got := filter.BearerToken(); got != configuredToken {
		t.Fatalf("BearerToken() after read failure = %q, want %q", got, configuredToken)
	}

	if strings.Contains(logs.String(), configuredToken) {
		t.Fatal("BearerToken() logged the bearer token")
	}

	if !filter.bearerTokenExpirationTime.After(time.Now()) {
		t.Fatalf("reload retry time = %v, want a future time", filter.bearerTokenExpirationTime)
	}

	expiresAt := time.Now().Add(time.Hour).Truncate(time.Second)
	refreshedToken, err := jwt.NewWithClaims(
		jwt.SigningMethodHS256,
		jwt.MapClaims{"exp": expiresAt.Unix()},
	).SignedString([]byte("test-key"))
	if err != nil {
		t.Fatal(err)
	}

	if err = os.WriteFile(tokenFile, []byte(refreshedToken), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := filter.BearerToken(); got != configuredToken {
		t.Fatalf("BearerToken() before retry delay = %q, want %q", got, configuredToken)
	}

	filter.bearerTokenExpirationTime = time.Time{}

	if got := filter.BearerToken(); got != refreshedToken {
		t.Fatalf("BearerToken() after successful reload = %q, want %q", got, refreshedToken)
	}

	if got := filter.bearerTokenExpirationTime; !got.Equal(expiresAt) {
		t.Fatalf("bearer token expiration = %v, want %v", got, expiresAt)
	}
}

func TestHandleRequestDoesNotLogBearerToken(t *testing.T) {
	t.Parallel()

	const configuredToken = "configured-token"

	var logs strings.Builder

	filter := &kubeFilter{
		bearerToken: configuredToken,
		log: funcr.New(func(_, args string) {
			logs.WriteString(args)
		}, funcr.Options{Verbosity: 10}),
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/pods", nil)
	filter.handleRequest(request, labels.Everything(), "alice")

	if strings.Contains(logs.String(), configuredToken) {
		t.Fatal("handleRequest() logged the bearer token")
	}

	if got := request.Header.Get("Authorization"); got != "Bearer "+configuredToken {
		t.Fatalf("Authorization header = %q, want bearer token", got)
	}
}
