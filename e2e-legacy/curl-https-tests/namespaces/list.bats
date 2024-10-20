#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../../libs/namespaces_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/serviceaccount_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/rolebinding_utils.bash"

setup() {
  create_tenant oil alice User
  create_namespace alice oil-dev
  create_namespace alice oil-staging
  create_namespace alice oil-production
  kubectl patch tenants.capsule.clastix.io oil --type=json -p '[{"op": "add", "path": "/spec/owners/1", "value": {"kind": "ServiceAccount", "name": "system:serviceaccount:default:sa"}}]'
  create_serviceaccount sa default
  token=$(KUBECONFIG=${HACK_DIR}/sa.kubeconfig kubectl config view -o json --raw -o jsonpath='{.users[?(@.name == "sa")].user.token}')
  endpoint=$(KUBECONFIG=${HACK_DIR}/sa.kubeconfig kubectl config view -o json --raw -o jsonpath='{.clusters[?(@.name == "kind-capsule")].cluster.server}')
}

teardown() {
  delete_tenant oil
}

@test "List allowed namespaces" {
  local sa="oil-dev
oil-production
oil-staging"
  poll_until_equals "SA" "$sa" "curl -s -H "Authorization: Bearer $token"  $endpoint/api/v1/namespaces | jq -r '.items[].metadata.name'" 3 5
}
