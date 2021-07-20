#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../../libs/namespaces_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/serviceaccount_utils.bash"

setup() {
  create_tenant byod alice User
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/nodeSelector", "value": {"kubernetes.io/hostname": "capsule-worker"}}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/1", "value": {"kind": "ServiceAccount", "name": "system:serviceaccount:byod-namespace:sa"}}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/2", "value": {"kind": "Group", "name": "foo.clastix.io"}}]'
  create_namespace alice byod-namespace
  create_serviceaccount sa byod-namespace

  create_tenant byodgroup foo.clastix.io Group
  kubectl patch tenants.capsule.clastix.io byodgroup --type=json -p '[{"op": "add", "path": "/spec/nodeSelector", "value": {"kubernetes.io/hostname": "capsule-worker2"}}]'

  create_tenant shared bob User
  create_namespace bob shared-namespace
}

teardown() {
  delete_tenant byod
  delete_tenant byodgroup
  delete_tenant shared
}

@test "Get node without permissions" {
  poll_until_equals "shared scenario" "" "kubectl --kubeconfig=${HACK_DIR}/bob.kubeconfig get node capsule-worker --output=name" 3 5
  poll_until_equals "BYOD scenario for User" "" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get node capsule-worker --output=name" 3 5
  poll_until_equals "BYOD scenario for SA" "" "kubectl kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get node capsule-worker --output=name" 3 5
  poll_until_equals "BYOD scenario for Group" "" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get node capsule-worker --output=name" 3 5
}

@test "Get node with List operation" {
  kubectl patch tenants.capsule.clastix.io shared --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/1/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/2/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byodgroup --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'

  echo "Get allowed node" >&3
  local list="node/capsule-worker"
  poll_until_equals "shared scenario" "$list" "kubectl --kubeconfig=${HACK_DIR}/bob.kubeconfig get node capsule-worker --output=name" 3 5
  poll_until_equals "BYOD scenario for User" "$list" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get node capsule-worker --output=name" 3 5
  poll_until_equals "BYOD scenario for SA" "$list" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get node capsule-worker --output=name" 3 5
  poll_until_equals "BYOD scenario for Group - node 1" "$list" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get node capsule-worker --output=name" 3 5
  poll_until_equals "BYOD scenario for Group - node 2" "node/capsule-worker2" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get node capsule-worker2 --output=name" 3 5

  echo "Get nonallowed node" >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get node capsule-control-plane
  [ $status -ne 0 ]
  [ "${lines[0]}" = 'Error from server (NotFound): nodes "capsule-control-plane" not found' ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get node capsule-worker2
  [ $status -ne 0 ]
  [ "${lines[0]}" = 'Error from server (NotFound): nodes "capsule-worker2" not found' ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get node capsule-control-plane
  [ $status -ne 0 ]
  [ "${lines[0]}" = 'Error from server (NotFound): nodes "capsule-control-plane" not found' ]
}
