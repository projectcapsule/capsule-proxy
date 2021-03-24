#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../libs/namespaces_utils.bash"

setup() {
  create_tenant node alice
  kubectl patch tenants.capsule.clastix.io node --type=json -p '[{"op": "add", "path": "/spec/nodeSelector", "value": {"kubernetes.io/hostname": "capsule-control-plane"}}]'
  kubectl label node capsule-control-plane capsule.clastix.io/tenant=node --overwrite
}

teardown() {
  delete_tenant node

  kubectl label node capsule-control-plane capsule.clastix.io/tenant-
  kubectl label node capsule-control-plane capsule.clastix.io/test-
  kubectl uncordon capsule-control-plane
  kubectl taint nodes capsule-control-plane key1=value1:NoSchedule-
}

@test "Nodes retrieval via kubectl" {
  poll_until_equals "no nodes" "" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get node capsule-control-plane --output=name" 3 5

  kubectl patch tenants.capsule.clastix.io node --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-node-listing": "true"}}]'
  local list="node/capsule-control-plane"
  poll_until_equals "nodes retrieval" "$list" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get node capsule-control-plane --output=name" 3 5
}

@test "Node labeling via kubectl" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl label node capsule-control-plane capsule.clastix.io/test=labeling"
  [ $status -eq 1 ]

  kubectl patch tenants.capsule.clastix.io node --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-node-listing": "true", "capsule.clastix.io/enable-node-update": "true"}}]'
  poll_until_equals "node labeling" "node/capsule-control-plane labeled" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl label node capsule-control-plane capsule.clastix.io/test=labeling" 3 5
}

@test "Node tainting via kubectl" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl taint nodes capsule-control-plane key1=value1:NoSchedule"
  [ $status -eq 1 ]

  kubectl patch tenants.capsule.clastix.io node --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-node-listing": "true", "capsule.clastix.io/enable-node-update": "true"}}]'
  poll_until_equals "node tainting" "node/capsule-control-plane tainted" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig
  kubectl taint nodes capsule-control-plane key1=value1:NoSchedule" 3 5
}

@test "Node cordoning via kubectl" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl cordon capsule-control-plane"
  [ $status -eq 1 ]

  kubectl patch tenants.capsule.clastix.io node --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-node-listing": "true", "capsule.clastix.io/enable-node-update": "true"}}]'
  poll_until_equals "node cordoning" "node/capsule-control-plane cordoned" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig
  kubectl cordon capsule-control-plane" 3 5
}

@test "Node deletion via kubectl" {
  if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -lt 19 ]]; then
    kubectl version
    skip "--dry-run=server is not supported on the following kubernetes version"
  fi

  kubectl patch tenants.capsule.clastix.io node --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-node-listing": "true", "capsule.clastix.io/enable-node-deletion": "true"}}]'
  poll_until_equals "node deletion" 'node "capsule-control-plane" deleted (server dry run)' "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl delete node capsule-control-plane --dry-run=server" 3 5
}
