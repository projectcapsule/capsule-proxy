#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../libs/serviceaccount_utils.bash"

setup() {
  create_serviceaccount sa default
  token=$(KUBECONFIG=${HACK_DIR}/sa.kubeconfig kubectl config view -o json --raw -o jsonpath='{.users[?(@.name == "sa")].user.token}')
  endpoint=$(KUBECONFIG=${HACK_DIR}/sa.kubeconfig kubectl config view -o json --raw -o jsonpath='{.clusters[?(@.name == "kind-capsule")].cluster.server}')
}
@test "Checking api-resources" {
  run sh -c "curl -s -H \"Authorization: Bearer $token\" $endpoint/apis"
  [ $status -eq 0 ]
}

@test "Checking version" {
  run sh -c "curl -s -H \"Authorization: Bearer $token\" $endpoint/version"
  [ $status -eq 0 ]
}