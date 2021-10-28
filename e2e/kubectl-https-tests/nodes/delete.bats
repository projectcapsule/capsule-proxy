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

@test "Delete node without permissions" {
  echo "Delete node without List and Delete operations" >&3
  run kubectl --kubeconfig=${HACK_DIR}/bob.kubeconfig delete node capsule-worker
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig delete node capsule-worker
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig delete node capsule-worker
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete node capsule-worker2
  [ $status -eq 1 ]

  echo "Delete node with only List operation" >&3
  kubectl patch tenants.capsule.clastix.io shared --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/1/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/2/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byodgroup --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'

  run kubectl --kubeconfig=${HACK_DIR}/bob.kubeconfig delete node capsule-worker
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig delete node capsule-worker
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig delete node capsule-worker
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete node capsule-worker2
  [ $status -eq 1 ]
}

@test "Delete node with List and Delete operations" {
  if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -lt 19 ]]; then
    kubectl version
    skip "--dry-run=server is not supported on the following kubernetes version"
  fi

  kubectl patch tenants.capsule.clastix.io shared --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List", "Delete"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List", "Delete"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/1/proxySettings","value":[{"kind": "Nodes", "operations": ["List", "Delete"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/2/proxySettings","value":[{"kind": "Nodes", "operations": ["List", "Delete"]}]}]'
  kubectl patch tenants.capsule.clastix.io byodgroup --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List", "Delete"]}]}]'

  echo "Delete allowed node" >&3
  poll_until_equals "shared scenario" 'node "capsule-worker" deleted (server dry run)' "kubectl --kubeconfig=${HACK_DIR}/bob.kubeconfig delete node capsule-worker --dry-run=server" 3 5
  poll_until_equals "BYOD scenario for User" 'node "capsule-worker" deleted (server dry run)' "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig delete node capsule-worker --dry-run=server" 3 5
  poll_until_equals "BYOD scenario for SA" 'node "capsule-worker" deleted (server dry run)' "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig delete node capsule-worker --dry-run=server" 3 5
  poll_until_equals "BYOD scenario for Group - node 1" 'node "capsule-worker" deleted (server dry run)' "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete node capsule-worker --dry-run=server" 3 5
  poll_until_equals "BYOD scenario for Group - node 2" 'node "capsule-worker2" deleted (server dry run)' "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete node capsule-worker2 --dry-run=server" 3 5

  echo "Delete nonallowed node" >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig delete node capsule-control-plane
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig delete node capsule-control-plane
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete node capsule-control-plane
  [ $status -eq 1 ]
}
