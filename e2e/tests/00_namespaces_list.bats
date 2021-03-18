#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../libs/namespaces_utils.bash"
load "$BATS_TEST_DIRNAME/../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../libs/tenants_utils.bash"

setup() {
  create_tenant oil alice
  create_namespace alice oil-dev
  create_namespace alice oil-staging
  create_namespace alice oil-production

  create_tenant gas bob
  create_namespace bob gas-qa
}

teardown() {
  delete_tenant oil
  delete_tenant gas
}

@test "Checking namespaces count via kubectl" {
  local gas="namespace/gas-qa"
  poll_until_equals "Checking kubectl output for gas" "$gas" "KUBECONFIG=${HACK_DIR}/bob.kubeconfig kubectl get namespaces --output=name" 3 5

  local oil="namespace/oil-dev
namespace/oil-production
namespace/oil-staging"
  poll_until_equals "Checking kubectl output for oil" "$oil" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get namespaces --output=name" 3 5
}
