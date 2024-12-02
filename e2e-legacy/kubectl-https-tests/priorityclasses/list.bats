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

@test "List priorityClasses without permissions" {
  poll_until_equals "User" "" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get priorityclasses.scheduling.k8s.io --output=name" 3 5
  poll_until_equals "SA" "" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get priorityclasses.scheduling.k8s.io --output=name" 3 5
  poll_until_equals "Group" "" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get priorityclasses.scheduling.k8s.io --output=name" 3 5
}

@test "List priorityClasses with List operation" {
  kubectl patch tenants.capsule.clastix.io priorityclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "PriorityClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io priorityclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/1/proxySettings","value":[{"kind": "PriorityClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io priorityclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/2/proxySettings","value":[{"kind": "PriorityClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io priorityclassgroup --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "PriorityClasses", "operations": ["List"]}]}]'

  local userlist="priorityclass.scheduling.k8s.io/custom
priorityclass.scheduling.k8s.io/maxpriority
priorityclass.scheduling.k8s.io/minpriority"

  local grouplist="priorityclass.scheduling.k8s.io/custom
priorityclass.scheduling.k8s.io/custom2
priorityclass.scheduling.k8s.io/maxpriority
priorityclass.scheduling.k8s.io/minpriority"

  poll_until_equals "User" "$userlist" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get priorityclasses.scheduling.k8s.io --output=name" 3 5
  poll_until_equals "SA" "$userlist" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get priorityclasses.scheduling.k8s.io --output=name" 3 5
  poll_until_equals "Group" "$grouplist" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get priorityclasses.scheduling.k8s.io --output=name" 3 5
}
