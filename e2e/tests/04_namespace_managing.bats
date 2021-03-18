#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../libs/namespaces_utils.bash"
load "$BATS_TEST_DIRNAME/../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../libs/tenants_utils.bash"

setup() {
  create_tenant oil alice
  create_namespace alice oil-app
}

teardown() {
  delete_tenant oil
}

@test "Managing a Deployment" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl --namespace oil-app create deployment --image redis:alpine my-cache"
  [ $status -eq 0 ]

  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl --namespace oil-app get deployment my-cache"
  [ $status -eq 0 ]
}

@test "Managing a Service" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl --namespace oil-app create service clusterip my-svc --tcp=5678:8080"
  [ $status -eq 0 ]

  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl --namespace oil-app get service my-svc"
  [ $status -eq 0 ]
}
