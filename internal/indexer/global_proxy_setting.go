// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package indexer

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
)

const (
	GlobalKindField = "spec.subjects.ownerkind"
)

// ProxySetting is the indexer that allows retrieving the Capsule Proxy Settings
// for a specific actor according to its kind.
type GlobalProxySetting struct{}

func (p GlobalProxySetting) Object() client.Object {
	return &v1beta1.GlobalProxySettings{}
}

func (p GlobalProxySetting) Field() string {
	return GlobalKindField
}

func (p GlobalProxySetting) Func() client.IndexerFunc {
	return func(object client.Object) (owners []string) {
		//nolint:forcetypeassert
		proxySetting := object.(*v1beta1.GlobalProxySettings)

		for _, owner := range proxySetting.Spec.Rules {
			for _, subject := range owner.Subjects {
				if subject.Kind == "" || subject.Name == "" {
					continue
				}

				owners = append(owners, fmt.Sprintf("%s:%s", subject.Kind.String(), subject.Name))
			}
		}

		return
	}
}
