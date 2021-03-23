#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../libs/namespaces_utils.bash"

setup() {
  create_tenant node alice
  kubectl patch tenants.capsule.clastix.io node --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-node-listing": "true"}}]'
  kubectl patch tenants.capsule.clastix.io node --type=json -p '[{"op": "add", "path": "/spec/nodeSelector", "value": {"kubernetes.io/hostname": "capsule-control-plane"}}]'
  kubectl label node capsule-control-plane capsule.clastix.io/tenant=node --overwrite
  create_namespace alice node-listing
}

teardown() {
  delete_tenant node

  kubectl label node capsule-control-plane capsule.clastix.io/tenant-
  kubectl label node capsule-control-plane capsule.clastix.io/test-
}

@test "Nodes retrieval via kubectl" {
  local list="node/capsule-control-plane"
  poll_until_equals "node retrieval" "$list" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get node capsule-control-plane --output=name" 3 5
}

@test "Nodes labeling via kubectl" {
  poll_until_equals "node labeling" "node/capsule-control-plane labeled" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl label node capsule-control-plane capsule.clastix.io/test=labeling" 3 5
}
