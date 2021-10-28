#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../../libs/namespaces_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/serviceaccount_utils.bash"

setup() {
  kubectl label nodes capsule-worker pool=cmp --overwrite
  kubectl label nodes capsule-worker2 pool=cmp2 --overwrite
  
  create_tenant byod alice User
  create_namespace alice byod-namespace

  create_tenant shared bob User
  create_namespace bob shared-namespace  

  create_serviceaccount sa byod-namespace
  create_tenant byodgroup foo.clastix.io Group
  
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/nodeSelector", "value": {"pool": "cmp"}}]'  
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/1", "value": {"kind": "ServiceAccount", "name": "system:serviceaccount:byod-namespace:sa"}}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/2", "value": {"kind": "Group", "name": "foo.clastix.io"}}]'
  kubectl patch tenants.capsule.clastix.io byodgroup --type=json -p '[{"op": "add", "path": "/spec/nodeSelector", "value": {"pool": "cmp2"}}]'
}

teardown() {
  delete_tenant byod
  delete_tenant byodgroup
  delete_tenant shared  
  kubectl label nodes capsule-worker pool-
  kubectl label nodes capsule-worker2 pool-
}

@test "Get node metrics with/without permissions" {
  echo "Allowing Listing..." >&3

  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/1/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/2/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byodgroup --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'  

  echo "Getting allowed node metrics" >&3

  poll_until_equals "Alice is getting metrics from allowed node" "capsule-worker" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig top node --no-headers capsule-worker | awk '{print \$1}'" 15 15
  poll_until_equals "SA is getting metrics from allowed node" "capsule-worker" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig top node --no-headers capsule-worker | awk '{print \$1}'" 15 15
  poll_until_equals "foo.clastix.io is getting metrics from allowed node" "capsule-worker" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig top node --no-headers capsule-worker | awk '{print \$1}'" 15 15


  echo "Getting non-allowed node metrics" >&3

  echo "Alice is getting metrics from non-allowed node..." >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig top node --no-headers capsule-worker2
  [ $status -ne 0 ]
  [ "${lines[0]}" = 'Error from server (NotFound): nodes "capsule-worker2" not found' ]

  echo "SA is getting metrics from non-allowed node..." >&3
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig top node --no-headers capsule-worker2
  [ $status -ne 0 ]
  [ "${lines[0]}" = 'Error from server (NotFound): nodes "capsule-worker2" not found' ]  

  echo "foo.clastix.io is getting metrics from non-allowed node..." >&3
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig top node --no-headers capsule-control-plane
  [ $status -ne 0 ]
  [ "${lines[0]}" = 'Error from server (NotFound): nodes "capsule-control-plane" not found' ] 
}
