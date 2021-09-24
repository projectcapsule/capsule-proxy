// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package request

import (
	h "net/http"
)

type Request interface {
	GetUserAndGroups() (string, []string, error)
	GetHTTPRequest() *h.Request
}
