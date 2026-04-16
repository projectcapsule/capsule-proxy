// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package request

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHTTP_processXFCC(t *testing.T) {
	t.Parallel()

	validCert := mustCreateTestCertificate(t, "alice", []string{"devs", "admins"})
	validEncodedCert := url.QueryEscape(validCert.pem)
	validHash := sha256Hex(validCert.cert.Raw)

	tests := []struct {
		name       string
		headerName string
		headerVals []string
		wantUser   string
		wantGroups []string
		wantErr    string
	}{
		{
			name:       "missing header",
			headerName: "X-Forwarded-Client-Cert",
			wantErr:    "no x-forwarded-client-cert header provided",
		},
		{
			name:       "multiple header values",
			headerName: "X-Forwarded-Client-Cert",
			headerVals: []string{"Cert=a", "Cert=b"},
			wantErr:    "multiple x-forwarded-client-cert headers are not allowed",
		},
		{
			name:       "multiple xfcc entries in one header",
			headerName: "X-Forwarded-Client-Cert",
			headerVals: []string{
				`Cert="` + validEncodedCert + `",Cert="` + validEncodedCert + `"`,
			},
			wantErr: "expected exactly one x-forwarded-client-cert entry",
		},
		{
			name:       "invalid xfcc header syntax",
			headerName: "X-Forwarded-Client-Cert",
			headerVals: []string{
				`Cert="abc`,
			},
			wantErr: "invalid x-forwarded-client-cert header",
		},
		{
			name:       "invalid field syntax",
			headerName: "X-Forwarded-Client-Cert",
			headerVals: []string{
				`Cert="` + validEncodedCert + `";BrokenField`,
			},
			wantErr: "invalid x-forwarded-client-cert fields",
		},
		{
			name:       "missing cert field",
			headerName: "X-Forwarded-Client-Cert",
			headerVals: []string{
				`Hash="` + validHash + `"`,
			},
			wantErr: "x-forwarded-client-cert missing Cert field",
		},
		{
			name:       "empty cert field",
			headerName: "X-Forwarded-Client-Cert",
			headerVals: []string{
				`Cert=""`,
			},
			wantErr: "x-forwarded-client-cert missing Cert field",
		},
		{
			name:       "invalid url escape in cert",
			headerName: "X-Forwarded-Client-Cert",
			headerVals: []string{
				`Cert="%ZZ"`,
			},
			wantErr: "invalid forwarded client certificate",
		},
		{
			name:       "non pem cert",
			headerName: "X-Forwarded-Client-Cert",
			headerVals: []string{
				`Cert="hello-world"`,
			},
			wantErr: "invalid forwarded client certificate: Cert field is not valid PEM",
		},
		{
			name:       "hash mismatch",
			headerName: "X-Forwarded-Client-Cert",
			headerVals: []string{
				`Hash="deadbeef";Cert="` + validEncodedCert + `"`,
			},
			wantErr: "forwarded client certificate hash mismatch",
		},
		{
			name:       "certificate missing username",
			headerName: "X-Forwarded-Client-Cert",
			headerVals: []string{
				`Cert="` + url.QueryEscape(mustCreateTestCertificate(t, "", []string{"devs"}).pem) + `"`,
			},
			wantErr: "forwarded client certificate does not contain a username",
		},
		{
			name:       "valid cert without hash",
			headerName: "X-Forwarded-Client-Cert",
			headerVals: []string{
				`Cert="` + validEncodedCert + `"`,
			},
			wantUser:   "alice",
			wantGroups: []string{"devs", "admins"},
		},
		{
			name:       "valid cert with hash",
			headerName: "X-Forwarded-Client-Cert",
			headerVals: []string{
				`Hash="` + strings.ToUpper(validHash) + `";Cert="` + validEncodedCert + `"`,
			},
			wantUser:   "alice",
			wantGroups: []string{"devs", "admins"},
		},
		{
			name:       "valid cert with extra spaces",
			headerName: "X-Forwarded-Client-Cert",
			headerVals: []string{
				`  Hash="` + validHash + `" ; Cert="` + validEncodedCert + `"  `,
			},
			wantUser:   "alice",
			wantGroups: []string{"devs", "admins"},
		},
		{
			name:       "custom header name",
			headerName: "X-Custom-XFCC",
			headerVals: []string{
				`Cert="` + validEncodedCert + `"`,
			},
			wantUser:   "alice",
			wantGroups: []string{"devs", "admins"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/", nil)
			for _, v := range tt.headerVals {
				req.Header.Add(tt.headerName, v)
			}

			h := http{
				Request:     req,
				xfcc_header: tt.headerName,
			}

			gotUser, gotGroups, err := h.processXFCC()
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if gotUser != tt.wantUser {
				t.Fatalf("expected username %q, got %q", tt.wantUser, gotUser)
			}
			if !reflect.DeepEqual(gotGroups, tt.wantGroups) {
				t.Fatalf("expected groups %v, got %v", tt.wantGroups, gotGroups)
			}
		})
	}
}

func TestCertIdentity(t *testing.T) {
	t.Parallel()

	cert := mustCreateTestCertificate(t, "bob", []string{"team-a", "team-b"}).cert

	user, groups := certIdentity(cert)
	if user != "bob" {
		t.Fatalf("expected username %q, got %q", "bob", user)
	}
	if !reflect.DeepEqual(groups, []string{"team-a", "team-b"}) {
		t.Fatalf("expected groups %v, got %v", []string{"team-a", "team-b"}, groups)
	}
}

func TestParseXFCCCert(t *testing.T) {
	t.Parallel()

	valid := mustCreateTestCertificate(t, "charlie", []string{"ops"})
	validEncoded := url.QueryEscape(valid.pem)

	tests := []struct {
		name    string
		value   string
		wantErr string
	}{
		{
			name:  "valid pem",
			value: validEncoded,
		},
		{
			name:    "invalid url escape",
			value:   "%ZZ",
			wantErr: "cannot url-decode Cert field",
		},
		{
			name:    "not pem",
			value:   url.QueryEscape("not-a-pem"),
			wantErr: "Cert field is not valid PEM",
		},
		{
			name:    "wrong pem block type",
			value:   url.QueryEscape(string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("abc")}))),
			wantErr: `unexpected PEM block type "RSA PRIVATE KEY"`,
		},
		{
			name:    "pem with trailing data",
			value:   url.QueryEscape(valid.pem + "\ntrailing"),
			wantErr: "Cert field contains trailing unexpected data",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cert, err := parseXFCCCert(tt.value)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if cert.Subject.CommonName != "charlie" {
				t.Fatalf("expected CN %q, got %q", "charlie", cert.Subject.CommonName)
			}
		})
	}
}

func TestVerifyXFCCHash(t *testing.T) {
	t.Parallel()

	cert := mustCreateTestCertificate(t, "dora", []string{"qa"}).cert
	valid := sha256Hex(cert.Raw)

	tests := []struct {
		name    string
		hash    string
		wantErr string
	}{
		{
			name: "matching lowercase",
			hash: valid,
		},
		{
			name: "matching uppercase with spaces",
			hash: "  " + strings.ToUpper(valid) + "  ",
		},
		{
			name:    "mismatch",
			hash:    "deadbeef",
			wantErr: "expected",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := verifyXFCCHash(cert, tt.hash)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestSplitXFCCEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		header  string
		want    []string
		wantErr string
	}{
		{
			name:   "single entry",
			header: `Hash="abc";Cert="def"`,
			want:   []string{`Hash="abc";Cert="def"`},
		},
		{
			name:   "two entries separated by comma",
			header: `Hash="abc";Cert="def",Hash="ghi";Cert="jkl"`,
			want:   []string{`Hash="abc";Cert="def"`, `Hash="ghi";Cert="jkl"`},
		},
		{
			name:   "comma inside quotes is preserved",
			header: `Subject="CN=alice,O=devs\,admins";Cert="def"`,
			want:   []string{`Subject="CN=alice,O=devs\,admins";Cert="def"`},
		},
		{
			name:    "unterminated quote",
			header:  `Subject="alice`,
			wantErr: "unterminated quote",
		},
		{
			name:    "dangling escape",
			header:  "\"abc\\",
			wantErr: "dangling escape",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := splitXFCCEntries(tt.header)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestParseXFCCFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entry   string
		want    map[string]string
		wantErr string
	}{
		{
			name:  "parses simple fields",
			entry: `Hash="abc";Cert="def"`,
			want: map[string]string{
				"Hash": "abc",
				"Cert": "def",
			},
		},
		{
			name:  "trims and skips empty parts",
			entry: ` ; Hash="abc" ; ; Cert="def" ; `,
			want: map[string]string{
				"Hash": "abc",
				"Cert": "def",
			},
		},
		{
			name:  "keeps unquoted value",
			entry: `Hash=abc;Cert=def`,
			want: map[string]string{
				"Hash": "abc",
				"Cert": "def",
			},
		},
		{
			name:  "quoted escaped value",
			entry: `Subject="CN=\"alice\"";Hash="abc"`,
			want: map[string]string{
				"Subject": `CN="alice"`,
				"Hash":    "abc",
			},
		},
		{
			name:    "missing equals",
			entry:   `Hash`,
			wantErr: "missing '=' in field",
		},
		{
			name:    "empty key",
			entry:   `=abc`,
			wantErr: "empty field name",
		},
		{
			name:    "invalid quoted value",
			entry:   `Subject="abc\`,
			wantErr: "invalid value for",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseXFCCFields(tt.entry)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestSplitOutsideQuotes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		sep     rune
		want    []string
		wantErr string
	}{
		{
			name:  "splits on separator outside quotes",
			input: `a,"b,c",d`,
			sep:   ',',
			want:  []string{"a", `"b,c"`, "d"},
		},
		{
			name:  "keeps escaped quote content",
			input: `"a\"b",c`,
			sep:   ',',
			want:  []string{`"a\"b"`, "c"},
		},
		{
			name:    "unterminated quote",
			input:   `"abc`,
			sep:     ',',
			wantErr: "unterminated quote",
		},
		{
			name:    "dangling escape in quotes",
			input:   `"abc\`,
			sep:     ',',
			wantErr: "dangling escape",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := splitOutsideQuotes(tt.input, tt.sep)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestUnquoteXFCCValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{
			name:  "quoted value",
			input: `"abc"`,
			want:  "abc",
		},
		{
			name:  "quoted escaped quote",
			input: `"a\"b"`,
			want:  `a"b`,
		},
		{
			name:  "unquoted value",
			input: `abc`,
			want:  "abc",
		},
		{
			name:    "dangling escape",
			input:   `"abc\` + `"`,
			wantErr: "dangling escape",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := unquoteXFCCValue(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestUnescapeQuoted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{
			name:  "plain text",
			input: "abc",
			want:  "abc",
		},
		{
			name:  "escaped quote",
			input: `a\"b`,
			want:  `a"b`,
		},
		{
			name:  "escaped backslash",
			input: `a\\b`,
			want:  `a\b`,
		},
		{
			name:  "unknown escape is preserved as char",
			input: `a\qb`,
			want:  `aqb`,
		},
		{
			name:    "dangling escape",
			input:   `abc\`,
			wantErr: "dangling escape in quoted string",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := unescapeQuoted(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

type testCertificate struct {
	cert *x509.Certificate
	pem  string
}

func mustCreateTestCertificate(t *testing.T, commonName string, organizations []string) testCertificate {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          mustSerialNumber(t),
		Subject:               pkix.Name{CommonName: commonName, Organization: organizations},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("failed to parse generated certificate: %v", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: der,
	})

	return testCertificate{
		cert: cert,
		pem:  string(pemBytes),
	}
}

func mustSerialNumber(t *testing.T) *big.Int {
	t.Helper()

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("failed to generate serial number: %v", err)
	}

	return serial
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
