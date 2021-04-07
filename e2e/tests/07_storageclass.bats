#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../libs/namespaces_utils.bash"
load "$BATS_TEST_DIRNAME/../libs/storageclass_utils.bash"

setup() {
  create_tenant storageclass alice
  kubectl patch tenants.capsule.clastix.io storageclass --type=json -p '[{"op": "add", "path": "/spec/storageClasses", "value": {"allowed": ["custom"], "allowedRegex": "\\w+fs"}}]'
}

teardown() {
  delete_tenant storageclass

  delete_storageclass cephfs || true
  delete_storageclass glusterfs || true
  delete_storageclass custom || true
  delete_storageclass nonallowed || true
}

@test "Storage Class retrieval via kubectl" {
  poll_until_equals "no StorageClass" "" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get storageclasses.storage.k8s.io --output=name" 3 5

  create_storageclass cephfs
  create_storageclass glusterfs
  create_storageclass custom
  create_storageclass nonallowed

  local list="storageclass.storage.k8s.io/cephfs
storageclass.storage.k8s.io/custom
storageclass.storage.k8s.io/glusterfs"
  poll_until_equals "StorageClass retrieval" "$list" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get storageclasses.storage.k8s.io --output=name" 3 5
}

@test "Listing Non-allowed StorageClass is denied" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get storageclasses.storage.k8s.io nonallowed"
  [ $status -ne 0 ]
  [ "${lines[0]}" = 'Error from server (NotFound): storageclasses.storage.k8s.io "nonallowed" not found' ]
}
