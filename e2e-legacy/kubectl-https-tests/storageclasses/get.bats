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

@test "Get storageClass without permissions" {
  poll_until_equals "User" "" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get storageclasses.storage.k8s.io custom --output=name" 3 5
  poll_until_equals "SA" "" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get storageclasses.storage.k8s.io custom --output=name" 3 5
  poll_until_equals "Group" "" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get storageclasses.storage.k8s.io custom --output=name" 3 5
}

@test "Get storageClass with List operation" {
  kubectl patch tenants.capsule.clastix.io storageclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "StorageClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io storageclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/1/proxySettings","value":[{"kind": "StorageClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io storageclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/2/proxySettings","value":[{"kind": "StorageClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io storageclassgroup --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "StorageClasses", "operations": ["List"]}]}]'

  echo "Get allowed storageClass" >&3
  local list="storageclass.storage.k8s.io/custom"
  poll_until_equals "User" "$list" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get storageclasses.storage.k8s.io custom --output=name" 3 5
  poll_until_equals "SA" "$list" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get storageclasses.storage.k8s.io custom --output=name" 3 5
  poll_until_equals "Group - storageClass 1" "storageclass.storage.k8s.io/custom2" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get storageclasses.storage.k8s.io custom2 --output=name" 3 5
  poll_until_equals "Group - storageClass 2" "$list" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get storageclasses.storage.k8s.io custom --output=name" 3 5

  echo "Get nonallowed storageClass" >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get storageclasses.storage.k8s.io nonallowed --output=name
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (NotFound): storageclasses.storage.k8s.io "nonallowed" not found' ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get storageclasses.storage.k8s.io nonallowed --output=name
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (NotFound): storageclasses.storage.k8s.io "nonallowed" not found' ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get storageclasses.storage.k8s.io nonallowed --output=name
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (NotFound): storageclasses.storage.k8s.io "nonallowed" not found' ]
}
