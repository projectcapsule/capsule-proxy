// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/pkg/errors"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	toolscache "k8s.io/client-go/tools/cache"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/request"
)

const (
	subjectIndex               = "subjectIndex"
	reflectionSubjectIndex     = "reflectionSubjectIndex"
	RoleBindingReflectionLabel = "reflection.proxy.projectcapsule.dev/enabled"
	roleKind                   = "Role"
)

type RoleBindingReflector struct {
	reader        client.Reader
	informerCache crcache.Cache

	resultsMu         sync.RWMutex
	resultsGeneration uint64
	results           map[string]cachedReflectionResult
}

type cachedReflectionResult struct {
	generation uint64
	tenants    []string
}

func NewRoleBindingReflector(ctx context.Context, cache crcache.Cache) (*RoleBindingReflector, error) {
	if err := cache.IndexField(ctx, &rbacv1.RoleBinding{}, subjectIndex, func(obj client.Object) []string {
		values, _ := OwnerRoleBindingsIndexFunc(obj)

		return values
	}); err != nil {
		return nil, errors.Wrap(err, "cannot index RoleBindings by subject")
	}

	if err := cache.IndexField(ctx, &rbacv1.RoleBinding{}, reflectionSubjectIndex, func(obj client.Object) []string {
		values, _ := ReflectionRoleBindingsIndexFunc(obj)

		return values
	}); err != nil {
		return nil, errors.Wrap(err, "cannot index reflected RoleBindings by subject")
	}

	return &RoleBindingReflector{reader: cache, informerCache: cache, results: map[string]cachedReflectionResult{}}, nil
}

func (r *RoleBindingReflector) GetUserNamespacesFromRequest(req request.Request) ([]string, error) {
	username, groups, _ := req.GetUserAndGroups()

	bindings, err := r.getRoleBindingsForSubject(req.GetHTTPRequest().Context(), username, groups, subjectIndex)
	if err != nil {
		return nil, err
	}

	namespaces := sets.New[string]()
	for _, binding := range bindings {
		namespaces.Insert(binding.Namespace)
	}

	return sets.List(namespaces), nil
}

func (r *RoleBindingReflector) Start(ctx context.Context) error {
	handler := toolscache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { r.invalidateResults() },
		UpdateFunc: func(any, any) { r.invalidateResults() },
		DeleteFunc: func(any) { r.invalidateResults() },
	}

	for _, obj := range []client.Object{&rbacv1.RoleBinding{}, &rbacv1.Role{}, &rbacv1.ClusterRole{}, &corev1.Namespace{}} {
		informer, err := r.informerCache.GetInformer(ctx, obj)
		if err != nil {
			return err
		}

		if _, err := informer.AddEventHandler(handler); err != nil {
			return err
		}
	}

	<-ctx.Done()

	return nil
}

// GetUserTenantNamesForResource resolves reflected RBAC permissions directly
// to tenant selector values using the cached Namespace objects.
func (r *RoleBindingReflector) GetUserTenantNamesForResource(ctx context.Context, username string, groups []string, verb, apiGroup, resource string) ([]string, error) {
	cacheKey := reflectionResultKey(username, groups, verb, apiGroup, resource)
	if result, ok := r.cachedResult(cacheKey); ok {
		return result, nil
	}

	generation := r.resultGeneration()

	namespaces, err := r.GetUserNamespacesForResource(ctx, username, groups, verb, apiGroup, resource)
	if err != nil {
		return nil, err
	}

	tenantNames := sets.New[string]()

	for _, namespace := range namespaces {
		ns := &corev1.Namespace{}
		if err := r.reader.Get(ctx, client.ObjectKey{Name: namespace}, ns); client.IgnoreNotFound(err) != nil {
			return nil, errors.Wrap(err, "Unable to find namespace in cache")
		} else if err != nil {
			continue
		}

		if tenantName := reflectedTenantName(ns); tenantName != "" {
			tenantNames.Insert(tenantName)
		}
	}

	result := sets.List(tenantNames)
	r.storeResult(cacheKey, result, generation)

	return result, nil
}

func reflectedTenantName(namespace *corev1.Namespace) string {
	if value := namespace.Labels[capsulemeta.NewTenantLabel]; value != "" {
		return value
	}

	if value := namespace.Labels[capsulemeta.TenantLabel]; value != "" {
		return value
	}

	for _, owner := range namespace.OwnerReferences {
		if owner.Kind == "Tenant" {
			return owner.Name
		}
	}

	return ""
}

// GetUserNamespacesForResource returns namespaces where the request subject is
// bound to a namespaced Role granting the requested verb on the API resource.
// Policy rules use Kubernetes RBAC matching semantics for '*' in verbs, API
// groups, and resources. Both namespaced Roles and ClusterRoles referenced by
// a namespaced RoleBinding are supported.
func (r *RoleBindingReflector) GetUserNamespacesForResource(ctx context.Context, username string, groups []string, verb, apiGroup, resource string) ([]string, error) {
	bindings, err := r.getRoleBindingsForSubject(ctx, username, groups, reflectionSubjectIndex)
	if err != nil {
		return nil, err
	}

	namespaces := sets.New[string]()

	for _, binding := range bindings {
		if binding.RoleRef.APIGroup != rbacv1.GroupName {
			continue
		}

		var role client.Object

		switch binding.RoleRef.Kind {
		case roleKind:
			role = &rbacv1.Role{}
		case "ClusterRole":
			role = &rbacv1.ClusterRole{}
		default:
			continue
		}

		key := client.ObjectKey{Name: binding.RoleRef.Name}
		if binding.RoleRef.Kind == roleKind {
			key.Namespace = binding.Namespace
		}

		if err := r.reader.Get(ctx, key, role); client.IgnoreNotFound(err) != nil {
			return nil, errors.Wrap(err, "Unable to find role in cache")
		} else if err != nil {
			continue
		}

		var rules []rbacv1.PolicyRule

		switch role := role.(type) {
		case *rbacv1.Role:
			rules = role.Rules
		case *rbacv1.ClusterRole:
			rules = role.Rules
		default:
			return nil, fmt.Errorf("expected RBAC role but got %T", role)
		}

		for i := range rules {
			if policyRuleAllows(rules[i], verb, apiGroup, resource) {
				namespaces.Insert(binding.Namespace)

				break
			}
		}
	}

	return sets.List(namespaces), nil
}

func (r *RoleBindingReflector) getRoleBindingsForSubject(ctx context.Context, username string, groups []string, indexName string) ([]*rbacv1.RoleBinding, error) {
	keys := []string{}

	if strings.HasPrefix(username, serviceaccount.ServiceAccountUsernamePrefix) {
		namespace, name, err := serviceaccount.SplitUsername(username)
		if err != nil {
			return nil, errors.Wrap(err, "Unable to parse serviceAccount name")
		}

		keys = append(keys, fmt.Sprintf("%s-%s-%s", capsulerbac.ServiceAccountOwner, namespace, name))
	} else {
		keys = append(keys, fmt.Sprintf("%s-%s", capsulerbac.UserOwner, username))
	}

	for _, group := range groups {
		keys = append(keys, fmt.Sprintf("%s-%s", capsulerbac.GroupOwner, group))
	}

	bindings := map[string]*rbacv1.RoleBinding{}

	for _, key := range keys {
		items := &rbacv1.RoleBindingList{}
		if err := r.reader.List(ctx, items, client.MatchingFields{indexName: key}); err != nil {
			return nil, errors.Wrap(err, "Unable to find rolebindings in subject index")
		}

		for i := range items.Items {
			binding := items.Items[i].DeepCopy()
			bindings[fmt.Sprintf("%s/%s", binding.Namespace, binding.Name)] = binding
		}
	}

	result := make([]*rbacv1.RoleBinding, 0, len(bindings))
	for _, binding := range bindings {
		result = append(result, binding)
	}

	return result, nil
}

func policyRuleAllows(rule rbacv1.PolicyRule, verb, apiGroup, resource string) bool {
	// LIST requests have no resource name. Kubernetes does not grant an
	// unfiltered list from a rule restricted with resourceNames.
	return len(rule.ResourceNames) == 0 &&
		matchesRBACValue(rule.Verbs, verb) &&
		matchesRBACValue(rule.APIGroups, apiGroup) &&
		matchesRBACValue(rule.Resources, resource)
}

func matchesRBACValue(values []string, requested string) bool {
	for _, value := range values {
		if value == "*" || value == requested {
			return true
		}
	}

	return false
}

func reflectionResultKey(username string, groups []string, verb, apiGroup, resource string) string {
	sortedGroups := append([]string(nil), groups...)
	sort.Strings(sortedGroups)

	return strings.Join([]string{username, strings.Join(sortedGroups, "\x00"), verb, apiGroup, resource}, "\x01")
}

func (r *RoleBindingReflector) cachedResult(key string) ([]string, bool) {
	r.resultsMu.RLock()
	defer r.resultsMu.RUnlock()

	result, ok := r.results[key]
	if !ok || result.generation != r.resultsGeneration {
		return nil, false
	}

	return append([]string(nil), result.tenants...), true
}

func (r *RoleBindingReflector) resultGeneration() uint64 {
	r.resultsMu.RLock()
	defer r.resultsMu.RUnlock()

	return r.resultsGeneration
}

func (r *RoleBindingReflector) storeResult(key string, result []string, generation uint64) {
	r.resultsMu.Lock()
	defer r.resultsMu.Unlock()

	if generation == r.resultsGeneration {
		r.results[key] = cachedReflectionResult{generation: generation, tenants: append([]string(nil), result...)}
	}
}

func (r *RoleBindingReflector) invalidateResults() {
	r.resultsMu.Lock()
	defer r.resultsMu.Unlock()

	r.resultsGeneration++
	clear(r.results)
}

func OwnerRoleBindingsIndexFunc(obj any) (result []string, err error) {
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

func ReflectionRoleBindingsIndexFunc(obj any) ([]string, error) {
	//nolint:forcetypeassert
	rb := obj.(*rbacv1.RoleBinding)
	if rb.Labels[RoleBindingReflectionLabel] != "true" {
		return nil, nil
	}

	return OwnerRoleBindingsIndexFunc(obj)
}
