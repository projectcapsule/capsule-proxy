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
  kubectl label node capsule-worker capsule.clastix.io/test_user- || true
  kubectl label node capsule-worker capsule.clastix.io/test_sa- || true
  kubectl label node capsule-worker capsule.clastix.io/test_group- || true
  kubectl label node capsule-worker2 capsule.clastix.io/test- || true
  kubectl label node capsule-control-plane capsule.clastix.io/test- || true
  kubectl taint node capsule-worker key2=value2:NoSchedule- || true
  kubectl taint node capsule-worker key3=value3:NoSchedule- || true
  kubectl taint node capsule-worker key4=value4:NoSchedule- || true
  kubectl taint node capsule-worker2 key2=value2:NoSchedule- || true
  kubectl taint node capsule-worker key5=value5:NoSchedule- || true
  kubectl uncordon capsule-worker || true
  kubectl uncordon capsule-worker2 || true
}

@test "Update nodes without permissions" {
  echo "Update nodes without List and Update operations" >&3
  run kubectl --kubeconfig=${HACK_DIR}/bob.kubeconfig label node capsule-worker capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig label node capsule-worker capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig label node capsule-worker capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig label node capsule-worker2 capsule.clastix.io/test=labeling
  [ $status -eq 1 ]

  echo "Update nodes with only List operation" >&3
  kubectl patch tenants.capsule.clastix.io shared --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/1/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/2/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io byodgroup --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List"]}]}]'

  run kubectl --kubeconfig=${HACK_DIR}/bob.kubeconfig label node capsule-worker capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig label node capsule-worker capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig label node capsule-worker capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig label node capsule-worker2 capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
}


@test "Update node with List and Update operations" {
  kubectl patch tenants.capsule.clastix.io shared --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List", "Update"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List", "Update"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/1/proxySettings","value":[{"kind": "Nodes", "operations": ["List", "Update"]}]}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/owners/2/proxySettings","value":[{"kind": "Nodes", "operations": ["List", "Update"]}]}]'
  kubectl patch tenants.capsule.clastix.io byodgroup --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "Nodes", "operations": ["List", "Update"]}]}]'

  echo "Update nonallowed node" >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig label node capsule-control-plane capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig label node capsule-control-plane capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig label node capsule-control-plane capsule.clastix.io/test=labeling
  [ $status -eq 1 ]

  echo "Label allowed node" >&3
  poll_until_equals "shared scenario" "node/capsule-control-plane labeled" "kubectl --kubeconfig=${HACK_DIR}/bob.kubeconfig label node capsule-control-plane capsule.clastix.io/test=labeling" 3 5
  poll_until_equals "BYOD scenario for User" "node/capsule-worker labeled" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig label node capsule-worker capsule.clastix.io/test_user=labeling" 3 5
  poll_until_equals "BYOD scenario for SA" "node/capsule-worker labeled" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig label node capsule-worker capsule.clastix.io/test_sa=labeling" 3 5
  poll_until_equals "BYOD scenario for Group - node 1" "node/capsule-worker labeled" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig label node capsule-worker capsule.clastix.io/test_group=labeling" 3 5
  poll_until_equals "BYOD scenario for Group - node 2" "node/capsule-worker2 labeled" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig label node capsule-worker2 capsule.clastix.io/test=labeling" 3 5

  echo "Taint allowed node" >&3
  poll_until_equals "shared scenario" "node/capsule-worker modified" "kubectl --kubeconfig=${HACK_DIR}/bob.kubeconfig taint node capsule-worker key5=value5:NoSchedule --overwrite" 3 5
  poll_until_equals "BYOD scenario for User" "node/capsule-worker modified" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig taint node capsule-worker key2=value2:NoSchedule --overwrite" 3 5
  poll_until_equals "BYOD scenario for SA" "node/capsule-worker modified" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig taint node capsule-worker key3=value3:NoSchedule --overwrite" 3 5
  poll_until_equals "BYOD scenario for Group - node 1" "node/capsule-worker modified" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig taint node capsule-worker key4=value4:NoSchedule --overwrite" 3 5
  poll_until_equals "BYOD scenario for Group - node 2" "node/capsule-worker2 modified" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig taint node capsule-worker2 key2=value2:NoSchedule --overwrite" 3 5

  echo "Cordon allowed node" >&3
  poll_until_equals "shared scenario" "node/capsule-worker cordoned" "kubectl --kubeconfig=${HACK_DIR}/bob.kubeconfig cordon capsule-worker" 3 5
  kubectl uncordon capsule-worker || true
  poll_until_equals "BYOD scenario for User" "node/capsule-worker cordoned" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig cordon capsule-worker" 3 5
  kubectl uncordon capsule-worker || true
  poll_until_equals "BYOD scenario for SA" "node/capsule-worker cordoned" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig cordon capsule-worker" 3 5
  kubectl uncordon capsule-worker || true
  poll_until_equals "BYOD scenario for Group - node 1" "node/capsule-worker cordoned" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig cordon capsule-worker" 3 5
  poll_until_equals "BYOD scenario for Group - node 2" "node/capsule-worker2 cordoned" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig cordon capsule-worker2" 3 5
}
