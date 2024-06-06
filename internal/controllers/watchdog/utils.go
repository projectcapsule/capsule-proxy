package watchdog

import (
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	"github.com/projectcapsule/capsule-proxy/internal/utils"
)

func API(config *rest.Config) ([]utils.ProxyGroupVersionKind, error) {
	discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(config)

	apiResourceLists, err := discoveryClient.ServerPreferredNamespacedResources()
	if err != nil {
		return nil, errors.Wrap(err, "cannot retrieve server's preferred namespaced resources")
	}

	var out []utils.ProxyGroupVersionKind

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
			if !sets.New[string]([]string(i.Verbs)...).HasAll("get", "list", "watch") {
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
