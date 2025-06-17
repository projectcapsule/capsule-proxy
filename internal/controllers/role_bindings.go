// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/projectcapsule/capsule-proxy/internal/request"
)

const subjectIndex = "subjectIndex"

type RoleBindingReflector struct {
	store     cache.Indexer
	reflector *cache.Reflector
}

func NewRoleBindingReflector(config *rest.Config, resyncPeriod time.Duration) (*RoleBindingReflector, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create kubernetes clientset")
	}

	watcher := cache.NewListWatchFromClient(clientset.RbacV1().RESTClient(), "rolebindings", "", fields.Everything())

	store := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{subjectIndex: OwnerRoleBindingsIndexFunc})

	reflector := cache.NewReflector(watcher, &rbacv1.RoleBinding{}, store, resyncPeriod)

	return &RoleBindingReflector{
		store:     store,
		reflector: reflector,
	}, nil
}

func (r *RoleBindingReflector) GetUserNamespacesFromRequest(req request.Request) ([]string, error) {
	var err error

	username, groups, _ := req.GetUserAndGroups()

	namespaces := sets.NewString()

	userOwnerKind := capsulev1beta2.UserOwner

	var userRoleBindings []interface{}

	if strings.HasPrefix(username, serviceaccount.ServiceAccountUsernamePrefix) {
		userOwnerKind = capsulev1beta2.ServiceAccountOwner

		namespace, name, splitErr := serviceaccount.SplitUsername(username)
		if splitErr != nil {
			return nil, errors.Wrap(splitErr, "Unable to parse serviceAccount name")
		}

		username = fmt.Sprintf("%s-%s", namespace, name)
	}

	userRoleBindings, err = r.store.ByIndex(subjectIndex, fmt.Sprintf("%s-%s", userOwnerKind, username))
	if err != nil {
		return nil, errors.Wrap(err, "Unable to find rolebindings in index for user")
	}

	for _, rb := range userRoleBindings {
		rbt, ok := rb.(*rbacv1.RoleBinding)
		if !ok {
			return nil, fmt.Errorf("expected *rbacv1.RoleBinding but got %T", rb)
		}

		namespaces.Insert(rbt.GetNamespace())
	}

	for _, group := range groups {
		groupRoleBindings, err := r.store.ByIndex(subjectIndex, fmt.Sprintf("%s-%s", capsulev1beta2.GroupOwner, group))
		if err != nil {
			return nil, errors.Wrap(err, "Unable to find rolebindings in index for groups")
		}

		for _, rb := range groupRoleBindings {
			rbt, ok := rb.(*rbacv1.RoleBinding)
			if !ok {
				return nil, fmt.Errorf("expected *rbacv1.RoleBinding but got %T", rb)
			}

			namespaces.Insert(rbt.GetNamespace())
		}
	}

	return namespaces.List(), nil
}

func (r *RoleBindingReflector) Start(ctx context.Context) error {
	r.reflector.Run(ctx.Done())

	return nil
}

func OwnerRoleBindingsIndexFunc(obj interface{}) (result []string, err error) {
	//nolint:forcetypeassert
	rb := obj.(*rbacv1.RoleBinding)

	for _, subject := range rb.Subjects {
		parts := []string{subject.Kind}

		if len(subject.Namespace) > 0 {
			parts = append(parts, subject.Namespace)
		}

		parts = append(parts, subject.Name)

		result = append(result, strings.Join(parts, "-"))
	}

	return result, nil
}
