#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../../libs/namespaces_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/priorityclass_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/serviceaccount_utils.bash"

setup() {
  create_tenant priorityclassuser alice User
  kubectl patch tenants.capsule.clastix.io priorityclassuser --type=json -p '[{"op": "add", "path": "/spec/priorityClasses", "value": {"allowed": ["custom"], "allowedRegex": "\\w+priority"}}]'
  kubectl patch tenants.capsule.clastix.io priorityclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/1", "value": {"kind": "ServiceAccount", "name": "system:serviceaccount:priorityclassuser-namespace:sa"}}]'
  kubectl patch tenants.capsule.clastix.io priorityclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/2", "value": {"kind": "Group", "name": "foo.clastix.io"}}]'
  create_namespace alice priorityclassuser-namespace
  create_serviceaccount sa priorityclassuser-namespace

  create_tenant priorityclassgroup foo.clastix.io Group
  kubectl patch tenants.capsule.clastix.io priorityclassgroup --type=json -p '[{"op": "add", "path": "/spec/priorityClasses", "value": {"allowed": ["custom2"]}}]'

  create_priorityclass maxpriority
  create_priorityclass minpriority
  create_priorityclass custom
  create_priorityclass custom2
  create_priorityclass nonallowed
}

teardown() {
  delete_tenant priorityclassuser
  delete_tenant priorityclassgroup

  delete_priorityclass maxpriority || true
  delete_priorityclass minpriority || true
  delete_priorityclass custom || true
  delete_priorityclass custom2 || true
  delete_priorityclass nonallowed || true
}

@test "Get priorityClass without permissions" {
  poll_until_equals "User" "" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get priorityclasses.scheduling.k8s.io custom --output=name" 3 5
  poll_until_equals "SA" "" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get priorityclasses.scheduling.k8s.io custom --output=name" 3 5
  poll_until_equals "Group" "" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get priorityclasses.scheduling.k8s.io custom --output=name" 3 5
}

@test "Get priorityClass with List operation" {
  kubectl patch tenants.capsule.clastix.io priorityclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "PriorityClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io priorityclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/1/proxySettings","value":[{"kind": "PriorityClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io priorityclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/2/proxySettings","value":[{"kind": "PriorityClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io priorityclassgroup --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "PriorityClasses", "operations": ["List"]}]}]'

  echo "Get allowed priorityClass" >&3
  local list="priorityclass.scheduling.k8s.io/custom"
  poll_until_equals "User" "$list" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get priorityclasses.scheduling.k8s.io custom --output=name" 3 5
  poll_until_equals "SA" "$list" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get priorityclasses.scheduling.k8s.io custom --output=name" 3 5
  poll_until_equals "Group - priorityClass 1" "priorityclass.scheduling.k8s.io/custom2" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get priorityclasses.scheduling.k8s.io custom2 --output=name" 3 5
  poll_until_equals "Group - priorityClass 2" "$list" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get priorityclasses.scheduling.k8s.io custom --output=name" 3 5

  echo "Get nonallowed priorityClass" >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get priorityclasses.scheduling.k8s.io nonallowed --output=name
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (NotFound): priorityclasses.scheduling.k8s.io "nonallowed" not found' ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get priorityclasses.scheduling.k8s.io nonallowed --output=name
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (NotFound): priorityclasses.scheduling.k8s.io "nonallowed" not found' ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get priorityclasses.scheduling.k8s.io nonallowed --output=name
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (NotFound): priorityclasses.scheduling.k8s.io "nonallowed" not found' ]
}
