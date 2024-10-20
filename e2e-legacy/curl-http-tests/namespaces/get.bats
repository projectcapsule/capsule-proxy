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
  endpoint=http://127.0.0.1:9001
}

teardown() {
  delete_tenant oil
}

@test "List allowed namespace" {
  poll_until_equals "SA" "oil-dev" "curl -s -H \"Authorization: Bearer $token\" $endpoint/api/v1/namespaces/oil-dev | jq -r '.metadata.name'" 3 5
  poll_until_equals "SA" "oil-production" "curl -s -H \"Authorization: Bearer $token\" $endpoint/api/v1/namespaces/oil-production | jq -r '.metadata.name'" 3 5
  poll_until_equals "SA" "oil-staging" "curl -s -H \"Authorization: Bearer $token\" $endpoint/api/v1/namespaces/oil-staging | jq -r '.metadata.name'" 3 5
}

@test "List non-allowed namespaces" {
  poll_until_equals "SA" "Forbidden" "curl -s -H \"Authorization: Bearer $token\"  $endpoint/api/v1/namespaces/kube-system | jq -r '.reason'" 3 5
}