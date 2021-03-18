#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../libs/namespaces_utils.bash"

setup() {
  create_tenant foo alice
  kubectl patch tenants.capsule.clastix.io foo --type=json -p '[{"op": "add", "path": "/spec/namespacesMetadata", "value": {"additionalLabels": {"foo": "bar"}}}]'
  create_namespace alice foo-filter

  create_tenant bizz alice
  kubectl patch tenants.capsule.clastix.io bizz --type=json -p '[{"op": "add", "path": "/spec/namespacesMetadata", "value": {"additionalLabels": {"bizz": "buzz"}}}]'
  create_namespace alice bizz-filter
}

teardown() {
  delete_tenant foo
  delete_tenant bizz
}

@test "Listing with labelSelector via kubectl" {
  local list="namespace/bizz-filter
namespace/foo-filter"
  poll_until_equals "list of all Namespaces of different tenants" "$list" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get namespaces --output=name" 3 5

  local list="namespace/foo-filter"
  poll_until_equals "selecting foo-filter Namespace" "$list" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get namespaces --output=name --selector foo=bar" 3 5

  local list="namespace/bizz-filter"
  poll_until_equals "selecting bizz-filter Namespace" "$list" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get namespaces --output=name --selector bizz=buzz" 3 5
}
