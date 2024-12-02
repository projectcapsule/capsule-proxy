#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../../libs/namespaces_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/serviceaccount_utils.bash"

setup() {
  create_tenant oil alice User
  kubectl patch tenants.capsule.clastix.io oil --type=json -p '[{"op": "add", "path": "/spec/owners/1", "value": {"kind": "ServiceAccount", "name": "system:serviceaccount:oil-app:sa"}}]'
  kubectl patch tenants.capsule.clastix.io oil --type=json -p '[{"op": "add", "path": "/spec/owners/2", "value": {"kind": "Group", "name": "foo.clastix.io"}}]'
  create_namespace alice oil-app
  create_serviceaccount sa oil-app

  create_tenant gas foo.clastix.io Group
  create_namespace joe gas-qa foo.clastix.io
}

teardown() {
  delete_tenant oil
  delete_tenant gas
}

@test "Create resources in namespace" {
  echo "Create deployment as User" >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig --namespace oil-app create deployment --image redis:alpine user-my-cache
  [ $status -eq 0 ]
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig --namespace oil-app get deployment user-my-cache
  [ $status -eq 0 ]

  echo "Create service as User" >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig --namespace oil-app create service clusterip user-my-svc --tcp=5678:8080
  [ $status -eq 0 ]
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig --namespace oil-app get service user-my-svc
  [ $status -eq 0 ]

  echo "Create deployment as SA" >&3
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig --namespace oil-app create deployment --image redis:alpine sa-my-cache
  [ $status -eq 0 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig --namespace oil-app get deployment sa-my-cache
  [ $status -eq 0 ]

  echo "Create service as SA" >&3
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig --namespace oil-app create service clusterip sa-my-svc --tcp=5678:8080
  [ $status -eq 0 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig --namespace oil-app get service sa-my-svc
  [ $status -eq 0 ]

  echo "Create deployment as Group 1" >&3
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig --namespace oil-app create deployment --image redis:alpine group-my-cache
  [ $status -eq 0 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig --namespace oil-app get deployment group-my-cache
  [ $status -eq 0 ]

  echo "Create deployment as Group 2" >&3
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig --namespace gas-qa create deployment --image redis:alpine group-my-cache
  [ $status -eq 0 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig --namespace gas-qa get deployment group-my-cache
  [ $status -eq 0 ]

  echo "Create service as Group 1" >&3
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig --namespace oil-app create service clusterip group-my-svc --tcp=5678:8080
  [ $status -eq 0 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig --namespace oil-app get service group-my-svc
  [ $status -eq 0 ]

  echo "Create service as Group 2" >&3
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig --namespace gas-qa create service clusterip group-my-svc --tcp=5678:8080
  [ $status -eq 0 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig --namespace gas-qa get service group-my-svc
  [ $status -eq 0 ]
}
