// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"strings"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	"github.com/pkg/errors"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/clastix/capsule-proxy/internal/request"
)

const subjectIndex = "subjectIndex"

type RoleBindingReflector struct {
	store     cache.Indexer
	reflector *cache.Reflector
}

func NewRoleBindingReflector(config *rest.Config) (*RoleBindingReflector, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create kubernetes clientset")
	}

	watcher := cache.NewListWatchFromClient(clientset.RbacV1().RESTClient(), "rolebindings", "", fields.Everything())

	store := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{subjectIndex: OwnerRoleBindingsIndexFunc})

	reflector := cache.NewReflector(watcher, &rbacv1.RoleBinding{}, store, 0)

	return &RoleBindingReflector{
		store:     store,
		reflector: reflector,
	}, nil
}

func (r *RoleBindingReflector) GetUserNamespacesFromRequest(req request.Request) ([]string, error) {
	var err error

	username, groups, _ := req.GetUserAndGroups()

	namespaces := sets.NewString()

	userOwnerKind := capsulev1beta1.UserOwner

	var userRoleBindings []interface{}

	if strings.HasPrefix(username, serviceaccount.ServiceAccountUsernamePrefix) {
		userOwnerKind = capsulev1beta1.ServiceAccountOwner

		_, username, err = serviceaccount.SplitUsername(username)
		if err != nil {
			return nil, errors.Wrap(err, "Unable to parse serviceAccount name")
		}
	}

	userRoleBindings, err = r.store.ByIndex(subjectIndex, fmt.Sprintf("%s-%s", userOwnerKind, username))
	if err != nil {
		return nil, errors.Wrap(err, "Unable to find rolebindings in index for user")
	}

	for _, rb := range userRoleBindings {
		rb := rb.(*rbacv1.RoleBinding)
		namespaces.Insert(rb.GetNamespace())
	}

	for _, group := range groups {
		groupRoleBindings, err := r.store.ByIndex(subjectIndex, fmt.Sprintf("%s-%s", capsulev1beta1.GroupOwner, group))
		if err != nil {
			return nil, errors.Wrap(err, "Unable to find rolebindings in index for groups")
		}

		for _, rb := range groupRoleBindings {
			rb := rb.(*rbacv1.RoleBinding)
			namespaces.Insert(rb.GetNamespace())
		}
	}

	return namespaces.List(), nil
}

func (r *RoleBindingReflector) Start(ctx context.Context) error {
	return r.reflector.ListAndWatch(ctx.Done())
}

func OwnerRoleBindingsIndexFunc(obj interface{}) (result []string, err error) {
	rb := obj.(*rbacv1.RoleBinding)

	for _, subject := range rb.Subjects {
		result = append(result, fmt.Sprintf("%s-%s", subject.Kind, subject.Name))
	}

	return result, nil
}
