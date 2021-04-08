#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../libs/namespaces_utils.bash"

setup() {
  create_tenant node alice
  kubectl patch tenants.capsule.clastix.io node --type=json -p '[{"op": "add", "path": "/spec/nodeSelector", "value": {"kubernetes.io/hostname": "capsule-worker"}}]'
}

teardown() {
  delete_tenant node

  kubectl label node capsule-worker capsule.clastix.io/test- || true
  kubectl uncordon capsule-worker || true
  kubectl taint nodes capsule-worker key1=value1:NoSchedule- || true
}

@test "Nodes retrieval via kubectl" {
  poll_until_equals "no nodes" "" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get node capsule-worker --output=name" 3 5

  kubectl patch tenants.capsule.clastix.io node --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-node-listing": "true"}}]'
  local list="node/capsule-worker"
  poll_until_equals "nodes retrieval" "$list" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get node capsule-worker --output=name" 3 5
}

@test "Listing Non-allowed Node is denied" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get node capsule-control-plane"
  [ $status -ne 0 ]
  [ "${lines[0]}" = 'Error from server (NotFound): nodes "capsule-control-plane" not found' ]
}

@test "Node labeling via kubectl" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl label node capsule-worker capsule.clastix.io/test=labeling"
  [ $status -eq 1 ]

  kubectl patch tenants.capsule.clastix.io node --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-node-listing": "true", "capsule.clastix.io/enable-node-update": "true"}}]'
  poll_until_equals "node labeling" "node/capsule-worker labeled" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl label node capsule-worker capsule.clastix.io/test=labeling" 3 5
}

@test "Node tainting via kubectl" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl taint nodes capsule-worker key1=value1:NoSchedule"
  [ $status -eq 1 ]

  kubectl patch tenants.capsule.clastix.io node --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-node-listing": "true", "capsule.clastix.io/enable-node-update": "true"}}]'
  poll_until_equals "node tainting" "node/capsule-worker tainted" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig
  kubectl taint nodes capsule-worker key1=value1:NoSchedule" 3 5
}

@test "Node cordoning via kubectl" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl cordon capsule-worker"
  [ $status -eq 1 ]

  kubectl patch tenants.capsule.clastix.io node --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-node-listing": "true", "capsule.clastix.io/enable-node-update": "true"}}]'
  poll_until_equals "node cordoning" "node/capsule-worker cordoned" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig
  kubectl cordon capsule-worker" 3 5
}

@test "Node deletion via kubectl" {
  if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -lt 19 ]]; then
    kubectl version
    skip "--dry-run=server is not supported on the following kubernetes version"
  fi

  kubectl patch tenants.capsule.clastix.io node --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-node-listing": "true", "capsule.clastix.io/enable-node-deletion": "true"}}]'
  poll_until_equals "node deletion" 'node "capsule-worker" deleted (server dry run)' "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl delete node capsule-worker --dry-run=server" 3 5
}
