// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package request

//go:generate stringer -type AuthType

type AuthType int

const (
	BearerToken AuthType = iota
	TLSCertificate
	Anonymous
)
