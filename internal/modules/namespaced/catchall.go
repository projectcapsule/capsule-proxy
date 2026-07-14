// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package namespaced

import (
	"fmt"

	capsulelabels "github.com/projectcapsule/capsule/pkg/api/meta"
	v1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/controllers"
	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
)

type catchall struct {
	path                  string
	group                 string
	version               string
	resource              string
	writer                client.Writer
	roleBindingsReflector *controllers.RoleBindingReflector
}

func CatchAll(writer client.Writer, roleBindingsReflector *controllers.RoleBindingReflector, path, group, version, resource string) modules.Module {
	return &catchall{
		path:                  path,
		group:                 group,
		version:               version,
		resource:              resource,
		writer:                writer,
		roleBindingsReflector: roleBindingsReflector,
	}
}

func (l catchall) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{}
}

func (l catchall) GroupKind() schema.GroupKind {
	return schema.GroupKind{}
}

func (l catchall) Path() string {
	return l.path
}

func (l catchall) Methods() []string {
	return []string{"get"}
}

func (l catchall) Handle(proxyTenants []*tenant.ProxyTenant, proxyRequest request.Request) (selector labels.Selector, err error) {
	user, groups, _ := proxyRequest.GetUserAndGroups()

	var sourceTenants []string

	for _, tnt := range proxyTenants {
		var allowed bool

		for _, ns := range tnt.Tenant.Status.Namespaces {
			sar := v1.SubjectAccessReview{}
			sar.Spec.User = user
			sar.Spec.Groups = groups
			sar.Spec.ResourceAttributes = &v1.ResourceAttributes{
				Namespace: ns,
				Verb:      "list",
				Group:     l.group,
				Version:   l.version,
				Resource:  l.resource,
			}

			if err = l.writer.Create(proxyRequest.GetHTTPRequest().Context(), &sar); err != nil {
				return nil, fmt.Errorf("unable to check if user can list %s/%s", l.group, l.resource)
			}

			allowed = sar.Status.Allowed

			break
		}

		if allowed {
			sourceTenants = append(sourceTenants, tnt.Tenant.Name)
		}
	}

	if l.roleBindingsReflector != nil {
		tenantNames, reflectionErr := l.roleBindingsReflector.GetUserTenantNamesForResource(
			proxyRequest.GetHTTPRequest().Context(), user, groups, "list", l.group, l.resource,
		)
		if reflectionErr != nil {
			return nil, reflectionErr
		}

		sourceTenants = append(sourceTenants, tenantNames...)
	}

	var r *labels.Requirement

	switch {
	case len(sourceTenants) > 0:
		r, err = labels.NewRequirement(capsulelabels.ManagedByCapsuleLabel, selection.In, sourceTenants)
	default:
		r, err = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}

	return labels.NewSelector().Add(*r), err
}
