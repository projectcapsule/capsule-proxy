#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../libs/namespaces_utils.bash"

setup() {
  create_tenant byod alice
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-node-listing": "true"}}]'
  kubectl patch tenants.capsule.clastix.io byod --type=json -p '[{"op": "add", "path": "/spec/nodeSelector", "value": {"kubernetes.io/hostname": "capsule-control-plane"}}]'
  kubectl label node capsule-control-plane capsule.clastix.io/tenant=byod --overwrite
  create_namespace alice byod-namespace

  create_tenant shared bob
  create_namespace bob shared-namespace
}

teardown() {
  delete_tenant byod
  delete_tenant shared

  kubectl label node capsule-control-plane capsule.clastix.io/tenant-
}

@test "Nodes listing allowed" {
  poll_until_equals "filtered Node count matching on BYOD scenario" "1" "curl -s --cacert ~/.local/share/mkcert/rootCA.pem --cert ./hack/alice-oil.crt --key ./hack/alice-oil.key 'https://127.0.0.1:9001/api/v1/nodes' | jq .items | jq length" 3 5
  poll_until_equals "filtered Node count matching on shared scenario" "0" "curl -s --cacert ~/.local/share/mkcert/rootCA.pem --cert ./hack/bob-gas.crt --key ./hack/bob-gas.key 'https://127.0.0.1:9001/api/v1/nodes' | jq .items | jq length" 3 5
}
