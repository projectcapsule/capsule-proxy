#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../libs/namespaces_utils.bash"

setup() {
  create_tenant byod alice
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-node-listing": "true"}}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/nodeSelector", "value": {"kubernetes.io/hostname": "capsule-control-plane"}}]'
  kubectl label node capsule-control-plane capsule.clastix.io/tenant=byod --overwrite
  create_namespace alice byod-namespace

  create_tenant shared bob
  create_namespace bob shared-namespace
}

teardown() {
  delete_tenant byod
  delete_tenant shared

  kubectl label node capsule-control-plane capsule.clastix.io/tenant-
}

@test "Nodes listing allowed via kubectl" {
  poll_until_equals "filtered Node on shared scenario" "" "KUBECONFIG=${HACK_DIR}/bob.kubeconfig kubectl get node --output=name" 3 5

  local list="node/capsule-control-plane"
  poll_until_equals "filtered Node on BYOD scenario" "$list" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get node --output=name" 3 5
}
