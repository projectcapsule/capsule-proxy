// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package middleware_test

import (
	"testing"

	"github.com/clastix/capsule-proxy/internal/webserver/middleware"
)

func TestCheckBearerToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token string
		want  bool
		err   bool
	}{
		{"fail no bearer", "alksjdas239ldasdasd123ljksadsj", false, false},
		{"pass jwt", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c", true, false},
		{"pass gcp access token", "Bearer ya29.A0AVA9y1tO6PluMRS3RY2zq2SjrZxHoMFgcuubgHq-yUNsoiFd8WkLiVHoxU_LtgbrHaYImH8qsM8IMvodoIFsW9gxVvPU8df6Hi7qKn76bem8ifDZ6VkSGl93nmDiDoVOEIBMEUeOgzxoUCwkLr2iFMvmtzzIhWnBpQqzkNnPtLXNGNFNGNFSDR65qe8oSrRP756QYC9GEAmbl8KRt0173", true, false},
		{"pass webtoken", "Bearer alksjdas2_9ldas-dasd123ljksadsj", true, false},
		{"fail space in token", "Bearer alksjdas239ld asdasd123lksadsj", false, false},
	}

	for _, eachTest := range tests {
		eachTest := eachTest
		t.Run(eachTest.name, func(t *testing.T) {
			t.Parallel()
			got, err := middleware.CheckBearerToken(eachTest.token)
			if got != eachTest.want {
				t.Errorf("got %v, want %v", got, eachTest.want)
			}
			if err != nil && !eachTest.err {
				t.Errorf("got error: %v", err)
			}
		})
	}
}
