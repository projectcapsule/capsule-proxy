package validation

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sdiscovery "k8s.io/client-go/discovery"

	capsuleproxyv1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
)

const wildcard = "*"

type DiscoveredClusterResource struct {
	APIGroup string
	Resource string
	Kinds    []string
	Verbs    []string
}

type ClusterResourceDiscoveryIndex struct {
	byGroup map[string]map[string]DiscoveredClusterResource
}

func (i *ClusterResourceDiscoveryIndex) groups() []string {
	groups := make([]string, 0, len(i.byGroup))

	for group := range i.byGroup {
		groups = append(groups, group)
	}

	slices.Sort(groups)

	return groups
}

func DiscoverClusterResources(discoveryClient k8sdiscovery.DiscoveryInterface) (*ClusterResourceDiscoveryIndex, error) {
	resourceLists, err := discoveryClient.ServerPreferredResources()
	if err != nil {
		var groupDiscoveryFailed *k8sdiscovery.ErrGroupDiscoveryFailed
		if !errors.As(err, &groupDiscoveryFailed) {
			return nil, err
		}
	}

	index := &ClusterResourceDiscoveryIndex{
		byGroup: make(map[string]map[string]DiscoveredClusterResource),
	}

	for _, resourceList := range resourceLists {
		groupVersion, err := schema.ParseGroupVersion(resourceList.GroupVersion)
		if err != nil {
			return nil, fmt.Errorf("cannot parse discovered groupVersion %q: %w", resourceList.GroupVersion, err)
		}

		group := groupVersion.Group

		if _, ok := index.byGroup[group]; !ok {
			index.byGroup[group] = make(map[string]DiscoveredClusterResource)
		}

		for _, apiResource := range resourceList.APIResources {
			if apiResource.Namespaced {
				continue
			}

			if strings.Contains(apiResource.Name, "/") {
				continue
			}

			current := index.byGroup[group][apiResource.Name]
			current.APIGroup = group
			current.Resource = apiResource.Name
			current.Kinds = appendUnique(current.Kinds, apiResource.Kind)
			current.Verbs = appendUnique(current.Verbs, apiResource.Verbs...)

			index.byGroup[group][apiResource.Name] = current
		}
	}

	return index, nil
}

func ValidateClusterResourceBlock(
	fieldPath string,
	clusterResource capsuleproxyv1beta1.ClusterResource,
	index *ClusterResourceDiscoveryIndex,
) error {
	var errs []error

	if len(clusterResource.APIGroups) == 0 {
		errs = append(errs, fmt.Errorf("%s.apiGroups: must not be empty", fieldPath))
	}

	if len(clusterResource.Resources) == 0 {
		errs = append(errs, fmt.Errorf("%s.resources: must not be empty", fieldPath))
	}

	if err := validateClusterResourceOperations(fieldPath, clusterResource); err != nil {
		errs = append(errs, err)
	}

	if hasWildcard(clusterResource.APIGroups) && len(clusterResource.APIGroups) > 1 {
		errs = append(errs, fmt.Errorf("%s.apiGroups: wildcard %q must not be combined with explicit API groups", fieldPath, wildcard))
	}

	if hasWildcard(clusterResource.Resources) && len(clusterResource.Resources) > 1 {
		errs = append(errs, fmt.Errorf("%s.resources: wildcard %q must not be combined with explicit resources", fieldPath, wildcard))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	apiGroups := clusterResource.APIGroups
	if hasWildcard(apiGroups) {
		apiGroups = index.groups()
	}

	for _, apiGroup := range apiGroups {
		resources, ok := index.byGroup[apiGroup]
		if !ok {
			errs = append(errs, fmt.Errorf("%s.apiGroups: API group %q was not discovered", fieldPath, apiGroup))

			continue
		}

		if hasWildcard(clusterResource.Resources) {
			for resourceName, resource := range resources {
				if err := validateDiscoveredClusterResource(fieldPath, apiGroup, resourceName, resource, clusterResource.EffectiveOperations()); err != nil {
					errs = append(errs, err)
				}
			}

			continue
		}

		for _, resourceName := range clusterResource.Resources {
			resource, ok := resources[resourceName]
			if !ok {
				errs = append(errs, fmt.Errorf(
					"%s.resources: resource %q was not discovered as a cluster-scoped resource in API group %q",
					fieldPath,
					resourceName,
					apiGroup,
				))

				continue
			}

			if err := validateDiscoveredClusterResource(fieldPath, apiGroup, resourceName, resource, clusterResource.EffectiveOperations()); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

func validateDiscoveredClusterResource(
	fieldPath string,
	apiGroup string,
	resourceName string,
	resource DiscoveredClusterResource,
	operations []capsuleproxyv1beta1.ClusterResourceOperation,
) error {
	var errs []error

	if len(resource.Kinds) == 0 {
		errs = append(errs, fmt.Errorf(
			"%s.resources: resource %q in API group %q has no discovered kind",
			fieldPath,
			resourceName,
			apiGroup,
		))
	}

	for _, operation := range operations {
		verb := strings.ToLower(operation.String())
		if slices.Contains(resource.Verbs, verb) {
			continue
		}

		errs = append(errs, fmt.Errorf(
			"%s.resources: resource %q in API group %q does not support %s",
			fieldPath,
			resourceName,
			apiGroup,
			operation,
		))
	}

	return errors.Join(errs...)
}

func validateClusterResourceOperations(
	fieldPath string,
	clusterResource capsuleproxyv1beta1.ClusterResource,
) error {
	var errs []error

	for operationIndex, operation := range clusterResource.Operations {
		if operation == capsuleproxyv1beta1.ClusterResourceOperationList ||
			operation == capsuleproxyv1beta1.ClusterResourceOperationGet {
			continue
		}

		errs = append(errs, fmt.Errorf(
			"%s.operations[%d]: unsupported operation %q, only %q and %q are supported",
			fieldPath,
			operationIndex,
			operation,
			capsuleproxyv1beta1.ClusterResourceOperationList,
			capsuleproxyv1beta1.ClusterResourceOperationGet,
		))
	}

	return errors.Join(errs...)
}

func hasWildcard(values []string) bool {
	return slices.Contains(values, wildcard)
}

func appendUnique[T comparable](values []T, candidates ...T) []T {
	for _, candidate := range candidates {
		if slices.Contains(values, candidate) {
			continue
		}

		values = append(values, candidate)
	}

	return values
}
