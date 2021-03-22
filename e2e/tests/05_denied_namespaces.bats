#!/usr/bin/env bats

@test "Getting default namespace is denied" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get namespace default"
  [ $status -ne 0 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): namespaces "default" is forbidden: User "alice" cannot get resource "namespaces" in API group "" in the namespace "default"' ]
}

@test "Listing Pods on default namespace is denied" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl --namespace default get pods"
  [ $status -ne 0 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): pods is forbidden: User "alice" cannot list resource "pods" in API group "" in the namespace "default"' ]
}

@test "Listing Services on default namespace is denied" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl --namespace default get services"
  [ $status -ne 0 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): services is forbidden: User "alice" cannot list resource "services" in API group "" in the namespace "default"' ]
}

@test "Listing ConfigMaps on default namespace is denied" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl --namespace default get configmaps"
  [ $status -ne 0 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): configmaps is forbidden: User "alice" cannot list resource "configmaps" in API group "" in the namespace "default"' ]
}

