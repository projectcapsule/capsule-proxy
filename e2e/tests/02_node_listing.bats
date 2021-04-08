#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../libs/namespaces_utils.bash"

setup() {
  create_tenant byod alice
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-node-listing": "true"}}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/nodeSelector", "value": {"kubernetes.io/hostname": "capsule-worker"}}]'
  create_namespace alice byod-namespace

  create_tenant shared bob
  create_namespace bob shared-namespace
}

teardown() {
  delete_tenant byod
  delete_tenant shared
}

@test "Nodes listing allowed via kubectl" {
  poll_until_equals "filtered Node on shared scenario" "" "KUBECONFIG=${HACK_DIR}/bob.kubeconfig kubectl get node --output=name" 3 5

  local list="node/capsule-worker"
  poll_until_equals "filtered Node on BYOD scenario" "$list" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get node --output=name" 3 5
}
