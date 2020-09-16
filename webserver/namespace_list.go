package webserver

import (
	"strings"
)

type NamespaceList []string

func (n NamespaceList) Len() int {
	return len(n)
}

func (n NamespaceList) Less(i, j int) bool {
	return strings.ToLower(n[i]) < strings.ToLower(n[j])
}

func (n NamespaceList) Swap(i, j int) {
	n[i], n[j] = n[j], n[i]
}
