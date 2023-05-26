// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
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
		if requirement.Matches(labels.Set(obj.GetLabels())) {
			return selector.Add(requirement), nil
		}
	}

	return nil, errors.NewNotFoundError(name, gv)
}

func HandleListSelector(requirements []labels.Requirement) (selector labels.Selector, err error) {
	selector = labels.NewSelector()

	requirementsMap := make(map[string]sets.String)
	generateRequirementsKey := func(requirement labels.Requirement) string { //nolint:nolintlint
		switch requirement.Operator() { //nolint:exhaustive
		case selection.Equals, selection.DoubleEquals, selection.In:
			return fmt.Sprintf("%s:%s", requirement.Key(), selection.In)
		case selection.NotEquals, selection.NotIn:
			return fmt.Sprintf("%s:%s", requirement.Key(), selection.NotIn)
		default:
			return fmt.Sprintf("%s:%s", requirement.Key(), requirement.Operator())
		}
	}

	for _, requirement := range requirements {
		key := generateRequirementsKey(requirement)

		if _, ok := requirementsMap[key]; !ok {
			requirementsMap[key] = requirement.Values()

			continue
		}

		requirementsMap[key] = requirementsMap[key].Union(requirement.Values())
	}

	for k, v := range requirementsMap {
		key, op := strings.Split(k, ":")[0], strings.Split(k, ":")[1]

		requirement, err := labels.NewRequirement(key, selection.Operator(op), v.List())
		if err != nil {
			return nil, err
		}

		selector = selector.Add(*requirement)
	}

	return selector, nil
}
