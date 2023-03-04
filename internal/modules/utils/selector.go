// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/modules/errors"
)

func HandleGetSelector(ctx context.Context, obj client.Object, client client.Client, requirements []labels.Requirement, name, kind string) (labels.Selector, error) {
	nf := func() error {
		group := obj.GetObjectKind().GroupVersionKind().Group
		if len(group) > 0 {
			group = fmt.Sprintf(".%s", group)
		}

		return errors.NewNotFoundError(
			fmt.Sprintf("%s%s %q not found", kind, group, name),
			&metav1.StatusDetails{
				Name:  name,
				Group: obj.GetObjectKind().GroupVersionKind().Group,
				Kind:  kind,
			},
		)
	}

	if err := client.Get(ctx, types.NamespacedName{Name: name}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nf()
		}

		return nil, err
	}

	selector := labels.NewSelector()

	for _, requirement := range requirements {
		selector = selector.Add(requirement)
	}

	if !selector.Matches(labels.Set(obj.GetLabels())) {
		return nil, nf()
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
