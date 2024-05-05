package utils

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ProxyGroupVersionKind struct {
	schema.GroupVersionKind
	// This must be used for path-based routing by the webserver filter.
	URLName string
}

func (g ProxyGroupVersionKind) Path() string {
	var parts []string

	if g.Group != "" {
		parts = append(parts, "apis")
		parts = append(parts, g.Group)
	} else {
		parts = append(parts, "api")
	}

	parts = append(parts, g.Version)
	parts = append(parts, g.URLName)

	return fmt.Sprintf("/%s", strings.Join(parts, "/"))
}

func (g ProxyGroupVersionKind) ResourcePath() string {
	return fmt.Sprintf("%s/{name}", g.Path())
}
