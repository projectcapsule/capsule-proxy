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


@test "List nodes without permissions" {
  poll_until_equals "shared scenario" "" "kubectl --kubeconfig=${HACK_DIR}/bob.kubeconfig get nodes --output=name" 3 5
  poll_until_equals "BYOD scenario for User" "" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get nodes --output=name" 3 5
  poll_until_equals "BYOD scenario for SA" "" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get nodes --output=name" 3 5
  poll_until_equals "BYOD scenario for Group" "" "kubectl kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get nodes --output=name" 3 5
}

@test "List node with List operation" {
  kubectl patch tenants.capsule.clastix.io shared --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/1/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/2/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byodgroup --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'

  local byodlist="node/capsule-worker"
  local sharedlist="node/capsule-control-plane
node/capsule-worker
node/capsule-worker2"
   local byodgrouplist="node/capsule-worker
node/capsule-worker2"

  poll_until_equals "shared scenario" "$sharedlist" "kubectl --kubeconfig=${HACK_DIR}/bob.kubeconfig get nodes --output=name" 3 5
  poll_until_equals "BYOD scenario for User" "$byodlist" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get nodes --output=name" 3 5
  poll_until_equals "BYOD scenario for SA" "$byodlist" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get nodes --output=name" 3 5
  poll_until_equals "BYOD scenario for Group" "$byodgrouplist" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get nodes --output=name" 3 5
}
