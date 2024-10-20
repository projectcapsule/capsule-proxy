#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../../libs/namespaces_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/serviceaccount_utils.bash"

setup() {
  kubectl label nodes capsule-worker pool=cmp --overwrite
  kubectl label nodes capsule-worker2 pool=cmp --overwrite
  
  create_tenant byod alice User
  create_namespace alice byod-namespace

  create_tenant shared bob User
  create_namespace bob shared-namespace  

  create_serviceaccount sa byod-namespace
  create_tenant byodgroup foo.clastix.io Group
  
  
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/1", "value": {"kind": "ServiceAccount", "name": "system:serviceaccount:byod-namespace:sa"}}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/2", "value": {"kind": "Group", "name": "foo.clastix.io"}}]'

  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/nodeSelector", "value": {"pool": "cmp"}}]'  
  kubectl patch tenants.capsule.clastix.io byodgroup --type=json -p '[{"op": "add", "path": "/spec/nodeSelector", "value": {"pool": "cmp"}}]'
}

teardown() {
  delete_tenant byod
  delete_tenant byodgroup
  delete_tenant shared  
  kubectl label nodes capsule-worker pool-
  kubectl label nodes capsule-worker2 pool-
}


@test "List node metrics with/without permissions" {
  echo "Listing non-allowed node metrics" >&3
  
  echo "Bob is listing metrics from non-allowed nodes..." >&3
  run kubectl --kubeconfig=${HACK_DIR}/bob.kubeconfig top nodes
  [ $status -ne 0 ]
  [ "${lines[0]}" = 'error: metrics not available yet' ]

  echo "Alice is listing metrics from non-allowed nodes..." >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig top nodes
  [ $status -ne 0 ]
  [ "${lines[0]}" = 'error: metrics not available yet' ]

  echo "SA is listing metrics from non-allowed nodes..." >&3
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig top nodes
  [ $status -ne 0 ]
  [ "${lines[0]}" = 'error: metrics not available yet' ]  

  echo "foo.clastix.io is listing metrics from non-allowed nodes..." >&3
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig top nodes
  [ $status -ne 0 ]
  [ "${lines[0]}" = 'error: metrics not available yet' ] 


  echo "Allowing Listing..." >&3

  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/1/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/2/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byodgroup --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'  

  echo "Listing allowed node metrics" >&3

  poll_until_equals "Alice is listing metrics from allowed node" "capsule-worker
capsule-worker2" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig top nodes --no-headers | awk '{print \$1}'" 15 15
  poll_until_equals "SA is listing metrics from allowed node" "capsule-worker
capsule-worker2" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig top nodes --no-headers | awk '{print \$1}'" 15 15
  poll_until_equals "foo.clastix.io is listing metrics from allowed node" "capsule-worker
capsule-worker2" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig top nodes --no-headers | awk '{print \$1}'" 15 15

}
