// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package indexer

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
)

const (
	SubjectKindField = "spec.subjects.ownerkind"
)

// ProxySetting is the indexer that allows retrieving the Capsule Proxy Settings
// for a specific actor according to its kind.
type ProxySetting struct{}

func (p ProxySetting) Object() client.Object {
	return &v1beta1.ProxySetting{}
}

func (p ProxySetting) Field() string {
	return SubjectKindField
}

func (p ProxySetting) Func() client.IndexerFunc {
	return func(object client.Object) (owners []string) {
		//nolint:forcetypeassert
		proxySetting := object.(*v1beta1.ProxySetting)

		for _, owner := range proxySetting.Spec.Subjects {
			owners = append(owners, fmt.Sprintf("%s:%s", owner.Kind.String(), owner.Name))
		}

		return
	}
}
