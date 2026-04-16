// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package request

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/url"
	"strings"
)

func (h http) processXFCC() (string, []string, error) {
	values := h.Header.Values(h.xfcc_header)
	if len(values) == 0 {
		return "", nil, NewErrUnauthorized("no x-forwarded-client-cert header provided")
	}

	if len(values) != 1 {
		return "", nil, NewErrUnauthorized("multiple x-forwarded-client-cert headers are not allowed")
	}

	entries, err := splitXFCCEntries(values[0])
	if err != nil {
		return "", nil, NewErrUnauthorized(fmt.Sprintf("invalid x-forwarded-client-cert header: %v", err))
	}

	if len(entries) != 1 {
		return "", nil, NewErrUnauthorized("expected exactly one x-forwarded-client-cert entry")
	}

	fields, err := parseXFCCFields(entries[0])
	if err != nil {
		return "", nil, NewErrUnauthorized(fmt.Sprintf("invalid x-forwarded-client-cert fields: %v", err))
	}

	certValue, ok := fields["Cert"]
	if !ok || certValue == "" {
		return "", nil, NewErrUnauthorized("x-forwarded-client-cert missing Cert field")
	}

	cert, err := parseXFCCCert(certValue)
	if err != nil {
		return "", nil, NewErrUnauthorized(fmt.Sprintf("invalid forwarded client certificate: %v", err))
	}

	if hashValue, ok := fields["Hash"]; ok && hashValue != "" {
		if err := verifyXFCCHash(cert, hashValue); err != nil {
			return "", nil, NewErrUnauthorized(fmt.Sprintf("forwarded client certificate hash mismatch: %v", err))
		}
	}

	username, groups := certIdentity(cert)
	if username == "" {
		return "", nil, NewErrUnauthorized("forwarded client certificate does not contain a username")
	}

	return username, groups, nil
}

func certIdentity(cert *x509.Certificate) (string, []string) {
	username := cert.Subject.CommonName

	groups := make([]string, 0, len(cert.Subject.Organization))
	groups = append(groups, cert.Subject.Organization...)

	return username, groups
}

func parseXFCCCert(v string) (*x509.Certificate, error) {
	decoded, err := url.QueryUnescape(v)
	if err != nil {
		return nil, fmt.Errorf("cannot url-decode Cert field: %w", err)
	}

	block, rest := pem.Decode([]byte(decoded))
	if block == nil {
		return nil, fmt.Errorf("cert field is not valid PEM")
	}

	if strings.TrimSpace(string(rest)) != "" {
		return nil, fmt.Errorf("cert field contains trailing unexpected data")
	}

	if block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("unexpected PEM block type %q", block.Type)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("cannot parse certificate: %w", err)
	}

	return cert, nil
}

func verifyXFCCHash(cert *x509.Certificate, provided string) error {
	sum := sha256.Sum256(cert.Raw)
	expected := strings.ToLower(hex.EncodeToString(sum[:]))
	got := strings.ToLower(strings.TrimSpace(provided))

	if got != expected {
		return fmt.Errorf("expected %s, got %s", expected, got)
	}

	return nil
}

func splitXFCCEntries(header string) ([]string, error) {
	return splitOutsideQuotes(header, ',')
}

func parseXFCCFields(entry string) (map[string]string, error) {
	parts, err := splitOutsideQuotes(entry, ';')
	if err != nil {
		return nil, err
	}

	fields := make(map[string]string, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("missing '=' in field %q", part)
		}

		key = strings.TrimSpace(key)

		value = strings.TrimSpace(value)

		if key == "" {
			return nil, fmt.Errorf("empty field name")
		}

		unquoted, err := unquoteXFCCValue(value)
		if err != nil {
			return nil, fmt.Errorf("invalid value for %q: %w", key, err)
		}

		fields[key] = unquoted
	}

	return fields, nil
}

func splitOutsideQuotes(s string, sep rune) ([]string, error) {
	var result []string

	var current strings.Builder

	inQuotes := false

	escaped := false

	for _, r := range s {
		switch {
		case escaped:
			current.WriteRune(r)

			escaped = false
		case r == '\\':
			if inQuotes {
				escaped = true
			} else {
				current.WriteRune(r)
			}

		case r == '"':
			inQuotes = !inQuotes

			current.WriteRune(r)

		case r == sep && !inQuotes:
			result = append(result, strings.TrimSpace(current.String()))

			current.Reset()

		default:
			current.WriteRune(r)
		}
	}

	if escaped {
		return nil, fmt.Errorf("dangling escape")
	}

	if inQuotes {
		return nil, fmt.Errorf("unterminated quote")
	}

	result = append(result, strings.TrimSpace(current.String()))

	return result, nil
}

func unquoteXFCCValue(v string) (string, error) {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return unescapeQuoted(v[1 : len(v)-1])
	}

	return v, nil
}

func unescapeQuoted(s string) (string, error) {
	var b strings.Builder

	escaped := false

	for _, r := range s {
		switch {
		case escaped:
			switch r {
			case '\\', '"':
				b.WriteRune(r)
			default:
				b.WriteRune(r)
			}

			escaped = false

		case r == '\\':
			escaped = true

		default:
			b.WriteRune(r)
		}
	}

	if escaped {
		return "", fmt.Errorf("dangling escape in quoted string")
	}

	return b.String(), nil
}
