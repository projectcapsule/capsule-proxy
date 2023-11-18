package watchdog

import (
	"strings"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

type GroupVersionKind struct {
	metav1.GroupVersionKind
	// This must be used for path-based routing by the webserver filter
	URLName string
}

func API(config *rest.Config) ([]GroupVersionKind, error) {
	discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(config)

	apiResourceLists, err := discoveryClient.ServerPreferredNamespacedResources()
	if err != nil {
		return nil, errors.Wrap(err, "cannot retrieve server's preferred namespaced resources")
	}

	var out []GroupVersionKind

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
			if !sets.New[string]([]string(i.Verbs)...).Has("get") {
				continue
			}

			out = append(out, GroupVersionKind{
				GroupVersionKind: metav1.GroupVersionKind{
					Group:   group,
					Version: version,
					Kind:    i.Kind},
				URLName: i.Name,
			})
		}
	}

	return out, nil
}
