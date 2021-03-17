#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../libs/namespaces_utils.bash"
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

@test "Checking namespaces count" {
  [ "$(curl -s --cacert ~/.local/share/mkcert/rootCA.pem --cert ./hack/bob-gas.crt --key ./hack/bob-gas.key "https://127.0.0.1:9001/api/v1/namespaces" | jq .items | jq length)" -eq "1" ]
  [ "$(curl -s --cacert ~/.local/share/mkcert/rootCA.pem --cert ./hack/alice-oil.crt --key ./hack/alice-oil.key "https://127.0.0.1:9001/api/v1/namespaces" | jq .items | jq length)" -eq "3" ]
}
