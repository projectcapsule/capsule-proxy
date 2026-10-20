// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

type hijackableResponse struct {
	*httptest.ResponseRecorder
	connection net.Conn
}

func (w hijackableResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.connection, bufio.NewReadWriter(bufio.NewReader(w.connection), bufio.NewWriter(w.connection)), nil
}

func TestTrackedResponseCapabilities(t *testing.T) {
	for _, mode := range []string{"untouched", "explicit-error", "implicit-body", "informational", "flush", "hijack", "unsupported-hijack"} {
		t.Run(mode, func(t *testing.T) {
			underlying := httptest.NewRecorder()
			writer := NewHTTPResponseWriter(underlying)
			wantWritten := true
			switch mode {
			case "untouched":
				wantWritten = false
			case "explicit-error":
				writer.WriteHeader(http.StatusInternalServerError)
			case "implicit-body":
				if _, err := writer.Write([]byte("handled")); err != nil {
					t.Fatal(err)
				}
			case "informational":
				writer.WriteHeader(http.StatusEarlyHints)
				wantWritten = false
			case "flush":
				if err := http.NewResponseController(writer).Flush(); err != nil || !underlying.Flushed {
					t.Fatalf("flush did not reach upstream writer: %v", err)
				}
			case "hijack":
				connection, peer := net.Pipe()
				t.Cleanup(func() { _ = connection.Close(); _ = peer.Close() })
				writer = NewHTTPResponseWriter(hijackableResponse{underlying, connection})
				actual, _, err := http.NewResponseController(writer).Hijack()
				if err != nil || actual != connection {
					t.Fatalf("hijack did not preserve connection: %v", err)
				}
			case "unsupported-hijack":
				if _, _, err := http.NewResponseController(writer).Hijack(); err == nil {
					t.Fatal("unsupported hijack succeeded")
				}
				wantWritten = false
			}
			if writer.Written() != wantWritten {
				t.Fatalf("written=%v, want %v", writer.Written(), wantWritten)
			}
		})
	}
}
