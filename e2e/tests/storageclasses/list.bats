#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../../libs/namespaces_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/storageclass_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/serviceaccount_utils.bash"

setup() {
  create_tenant storageclassuser alice User
  kubectl patch tenants.capsule.clastix.io storageclassuser --type=json -p '[{"op": "add", "path": "/spec/storageClasses", "value": {"allowed": ["custom"], "allowedRegex": "\\w+fs"}}]'
  kubectl patch tenants.capsule.clastix.io storageclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/1", "value": {"kind": "ServiceAccount", "name": "system:serviceaccount:storageclassuser-namespace:sa"}}]'
  kubectl patch tenants.capsule.clastix.io storageclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/2", "value": {"kind": "Group", "name": "foo.clastix.io"}}]'
  create_namespace alice storageclassuser-namespace
  create_serviceaccount sa storageclassuser-namespace

  create_tenant storageclassgroup foo.clastix.io Group
  kubectl patch tenants.capsule.clastix.io storageclassgroup --type=json -p '[{"op": "add", "path": "/spec/storageClasses", "value": {"allowed": ["custom2"]}}]'

  create_storageclass cephfs
  create_storageclass glusterfs
  create_storageclass custom
  create_storageclass custom2
  create_storageclass nonallowed
}

teardown() {
  delete_tenant storageclassuser
  delete_tenant storageclassgroup

  delete_storageclass cephfs || true
  delete_storageclass glusterfs || true
  delete_storageclass custom || true
  delete_storageclass custom2 || true
  delete_storageclass nonallowed || true
}

@test "List storageClasses without permissions" {
  poll_until_equals "User" "" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get storageclasses.storage.k8s.io --output=name" 3 5
  poll_until_equals "SA" "" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get storageclasses.storage.k8s.io --output=name" 3 5
  poll_until_equals "Group" "" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get storageclasses.storage.k8s.io --output=name" 3 5
}

@test "List storageClasses with List operation" {
  kubectl patch tenants.capsule.clastix.io storageclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "StorageClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io storageclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/1/proxySettings","value":[{"kind": "StorageClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io storageclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/2/proxySettings","value":[{"kind": "StorageClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io storageclassgroup --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "StorageClasses", "operations": ["List"]}]}]'

  local userlist="storageclass.storage.k8s.io/cephfs
storageclass.storage.k8s.io/custom
storageclass.storage.k8s.io/glusterfs"

  local grouplist="storageclass.storage.k8s.io/cephfs
storageclass.storage.k8s.io/custom
storageclass.storage.k8s.io/custom2
storageclass.storage.k8s.io/glusterfs"

  poll_until_equals "User" "$userlist" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get storageclasses.storage.k8s.io --output=name" 3 5
  poll_until_equals "SA" "$userlist" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get storageclasses.storage.k8s.io --output=name" 3 5
  poll_until_equals "Group" "$grouplist" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get storageclasses.storage.k8s.io --output=name" 3 5
}
