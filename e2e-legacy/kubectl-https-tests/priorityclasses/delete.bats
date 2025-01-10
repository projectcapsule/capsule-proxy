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

@test "Delete priorityClass without permissions" {
  echo "Delete priorityClass without List and Delete operations" >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig delete priorityclasses.scheduling.k8s.io custom
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig delete priorityclasses.scheduling.k8s.io maxpriority
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete priorityclasses.scheduling.k8s.io custom2
  [ $status -eq 1 ]

  echo "Delete priorityClass with only List operation" >&3
  kubectl patch tenants.capsule.clastix.io priorityclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "PriorityClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io priorityclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/1/proxySettings","value":[{"kind": "PriorityClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io priorityclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/2/proxySettings","value":[{"kind": "PriorityClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io priorityclassgroup --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "PriorityClasses", "operations": ["List"]}]}]'

  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig delete priorityclasses.scheduling.k8s.io custom
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig delete priorityclasses.scheduling.k8s.io maxpriority
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete priorityclasses.scheduling.k8s.io custom2
  [ $status -eq 1 ]
}

@test "Delete priorityClass with List and Delete operations" {
  kubectl patch tenants.capsule.clastix.io priorityclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "PriorityClasses", "operations": ["List", "Delete"]}]}]'
  kubectl patch tenants.capsule.clastix.io priorityclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/1/proxySettings","value":[{"kind": "PriorityClasses", "operations": ["List", "Delete"]}]}]'
  kubectl patch tenants.capsule.clastix.io priorityclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/2/proxySettings","value":[{"kind": "PriorityClasses", "operations": ["List", "Delete"]}]}]'
  kubectl patch tenants.capsule.clastix.io priorityclassgroup --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "PriorityClasses", "operations": ["List", "Delete"]}]}]'

  echo "Delete allowed priorityClass" >&3
  poll_until_equals "User" 'priorityclass.scheduling.k8s.io "custom" deleted' "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig delete priorityclasses.scheduling.k8s.io custom" 3 5
  poll_until_equals "SA" 'priorityclass.scheduling.k8s.io "maxpriority" deleted' "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig delete priorityclasses.scheduling.k8s.io maxpriority" 3 5
  poll_until_equals "Group - priorityClass 1" 'priorityclass.scheduling.k8s.io "custom2" deleted' "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete priorityclasses.scheduling.k8s.io custom2" 3 5
  poll_until_equals "Group - priorityClass 2" 'priorityclass.scheduling.k8s.io "minpriority" deleted' "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete priorityclasses.scheduling.k8s.io minpriority" 3 5

  echo "Delete nonallowed priorityClass" >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig delete priorityclasses.scheduling.k8s.io nonallowed
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig delete priorityclasses.scheduling.k8s.io nonallowed
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete priorityclasses.scheduling.k8s.io nonallowed
  [ $status -eq 1 ]
}
