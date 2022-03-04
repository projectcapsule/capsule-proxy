// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"testing"
)

func TestCheckBearerToken(t *testing.T) {
	var tests = []struct {
		name  string
		token string
		want  bool
		err   bool
	}{
		{"fail no bearer", "alksjdas239ldasdasd123ljksadsj", false, false},
		{"pass jwt", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c", true, false},
		{"pass webtoken", "Bearer alksjdas2_9ldas-dasd123ljksadsj", true, false},
		{"fail space in token", "Bearer alksjdas239ld asdasd123lksadsj", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checkBearerToken(tt.token)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
			if err != nil && !tt.err {
				t.Errorf("got error: %v", err)
			}
		})
	}
}
