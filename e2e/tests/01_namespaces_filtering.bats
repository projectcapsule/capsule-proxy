#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../libs/namespaces_utils.bash"

setup() {
  create_tenant foo alice
  kubectl patch tenants.capsule.clastix.io foo --type=json -p '[{"op": "add", "path": "/spec/namespacesMetadata", "value": {"additionalLabels": {"foo": "bar"}}}]'
  create_namespace alice foo-filter

  create_tenant bizz alice
  kubectl patch tenants.capsule.clastix.io bizz --type=json -p '[{"op": "add", "path": "/spec/namespacesMetadata", "value": {"additionalLabels": {"bizz": "buzz"}}}]'
  create_namespace alice bizz-filter
}

teardown() {
  delete_tenant foo
  delete_tenant bizz
}

@test "Listing with labelSelector" {
  # all namespaces
	[ "$(curl -s --cacert ~/.local/share/mkcert/rootCA.pem --cert ./hack/alice-oil.crt --key ./hack/alice-oil.key "https://127.0.0.1:9001/api/v1/namespaces" | jq .items | jq length)" -eq "2" ]
  # foo
	[ "$(curl -s --cacert ~/.local/share/mkcert/rootCA.pem --cert ./hack/alice-oil.crt --key ./hack/alice-oil.key "https://127.0.0.1:9001/api/v1/namespaces?labelSelector=foo=bar" | jq .items | jq length)" -eq "1" ]
	[ "$(curl -s --cacert ~/.local/share/mkcert/rootCA.pem --cert ./hack/alice-oil.crt --key ./hack/alice-oil.key "https://127.0.0.1:9001/api/v1/namespaces?labelSelector=foo=bar" | jq -r ".items[0].metadata.name")" = "foo-filter" ]
  # bizz
	[ "$(curl -s --cacert ~/.local/share/mkcert/rootCA.pem --cert ./hack/alice-oil.crt --key ./hack/alice-oil.key "https://127.0.0.1:9001/api/v1/namespaces?labelSelector=bizz=buzz" | jq .items | jq length)" -eq "1" ]
	[ "$(curl -s --cacert ~/.local/share/mkcert/rootCA.pem --cert ./hack/alice-oil.crt --key ./hack/alice-oil.key "https://127.0.0.1:9001/api/v1/namespaces?labelSelector=bizz=buzz" | jq -r ".items[0].metadata.name")" = "bizz-filter" ]
}
