// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/modules/errors"
)

func HandleGetSelector(ctx context.Context, obj client.Object, client client.Reader, requirements []labels.Requirement, name string, gv schema.GroupKind) (labels.Selector, error) {
	if err := client.Get(ctx, types.NamespacedName{Name: name}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errors.NewNotFoundError(name, gv)
		}

		return nil, err
	}

	selector := labels.NewSelector()

	for _, requirement := range requirements {
		selector = selector.Add(requirement)
	}

	if !selector.Matches(labels.Set(obj.GetLabels())) {
		return nil, errors.NewNotFoundError(name, gv)
	}

	return selector, nil
}

func HandleListSelector(requirements []labels.Requirement) (selector labels.Selector, err error) {
	selector = labels.NewSelector()

	for _, requirement := range requirements {
		selector = selector.Add(requirement)
	}

	return selector, nil
}
