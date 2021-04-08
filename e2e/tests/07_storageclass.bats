#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../libs/namespaces_utils.bash"
load "$BATS_TEST_DIRNAME/../libs/storageclass_utils.bash"

setup() {
  create_tenant storageclass alice
  kubectl patch tenants.capsule.clastix.io storageclass --type=json -p '[{"op": "add", "path": "/spec/storageClasses", "value": {"allowed": ["custom"], "allowedRegex": "\\w+fs"}}]'

  create_storageclass cephfs
  create_storageclass glusterfs
  create_storageclass custom
  create_storageclass nonallowed
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

  kubectl patch tenants.capsule.clastix.io storageclass --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-storageclass-listing": "true"}}]'

  local list="storageclass.storage.k8s.io/cephfs
storageclass.storage.k8s.io/custom
storageclass.storage.k8s.io/glusterfs"
  poll_until_equals "StorageClass retrieval" "$list" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get storageclasses.storage.k8s.io --output=name" 3 5
}

@test "Getting Non-allowed StorageClass is denied" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get storageclasses.storage.k8s.io nonallowed"
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (NotFound): storageclasses.storage.k8s.io "nonallowed" not found' ]
}

@test "Patching StorageClass via kubectl" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl label storageclasses.storage.k8s.io cephfs capsule.clastix.io/test=labeling"
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (NotFound): storageclasses.storage.k8s.io "cephfs" not found' ]

  kubectl patch tenants.capsule.clastix.io storageclass --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-storageclass-listing": "true"}}]'
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl label storageclasses.storage.k8s.io cephfs capsule.clastix.io/test=labeling"
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): storageclasses.storage.k8s.io "cephfs" is forbidden: User "alice" cannot patch resource "storageclasses" in API group "storage.k8s.io" at the cluster scope' ]

  kubectl patch tenants.capsule.clastix.io storageclass --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-storageclass-listing": "true", "capsule.clastix.io/enable-storageclass-update": "true"}}]'
  poll_until_equals "storageClass patching" "storageclass.storage.k8s.io/cephfs labeled" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl label storageclasses.storage.k8s.io cephfs capsule.clastix.io/test=labeling" 3 5
}

@test "Deleting StorageClass via kubectl" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl delete storageclasses.storage.k8s.io custom"
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): storageclasses.storage.k8s.io "custom" is forbidden: User "alice" cannot delete resource "storageclasses" in API group "storage.k8s.io" at the cluster scope' ]

  kubectl patch tenants.capsule.clastix.io storageclass --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-storageclass-listing": "true", "capsule.clastix.io/enable-storageclass-deletion": "true"}}]'
  poll_until_equals "storageClass deletion" 'storageclass.storage.k8s.io "custom" deleted' "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl delete storageclasses.storage.k8s.io custom" 3 5
}
