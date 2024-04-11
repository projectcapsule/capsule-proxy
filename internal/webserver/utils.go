package webserver

import (
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/discovery"

	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/utils"
)

func moduleGroupKindPresent(modules []modules.Module, clusterModule utils.ProxyGroupVersionKind) (present bool) {
	present = false

	for _, mod := range modules {
		if mod.GroupKind().Group == clusterModule.Group && mod.GroupKind().Kind == clusterModule.URLName {
			present = true

			break
		}
	}

	return
}

func serverPreferredResources(discoveryClient *discovery.DiscoveryClient) (out []utils.ProxyGroupVersionKind, err error) {
	apiResourceLists, err := discoveryClient.ServerPreferredResources()
	if err != nil {
		return nil, errors.Wrap(err, "cannot retrieve server's preferred resources")
	}

	for _, ar := range apiResourceLists {
		parts := strings.Split(ar.GroupVersion, "/")

		var group, version string

		if len(parts) == 1 {
			group = ""
			version = ar.GroupVersion
		} else {
			group = parts[0]
			version = parts[1]
		}

		for _, i := range ar.APIResources {
			// Skip namespaced resources
			if i.Namespaced {
				continue
			}

			if !sets.New[string]([]string(i.Verbs)...).Has("get") {
				continue
			}

			out = append(out, utils.ProxyGroupVersionKind{
				GroupVersionKind: schema.GroupVersionKind{
					Group:   group,
					Version: version,
					Kind:    i.Kind,
				},
				URLName: i.Name,
			})
		}
	}

	return out, nil
}
