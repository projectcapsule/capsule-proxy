// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package request

type ErrUnauthorized struct {
	message string
}

func NewErrUnauthorized(message string) *ErrUnauthorized {
	return &ErrUnauthorized{
		message: message,
	}
}

func (e *ErrUnauthorized) Error() string {
	return e.message
}
