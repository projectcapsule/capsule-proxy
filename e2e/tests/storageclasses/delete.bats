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

@test "Delete storageClass without permissions" {
  echo "Delete storageClass without List and Delete operations" >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig delete storageclasses.storage.k8s.io custom
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig delete storageclasses.storage.k8s.io cephfs
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete storageclasses.storage.k8s.io custom2
  [ $status -eq 1 ]

  echo "Delete storageClass with only List operation" >&3
  kubectl patch tenants.capsule.clastix.io storageclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "StorageClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io storageclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/1/proxySettings","value":[{"kind": "StorageClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io storageclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/2/proxySettings","value":[{"kind": "StorageClasses", "operations": ["List"]}]}]'
  kubectl patch tenants.capsule.clastix.io storageclassgroup --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "StorageClasses", "operations": ["List"]}]}]'

  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig delete storageclasses.storage.k8s.io custom
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig delete storageclasses.storage.k8s.io cephfs
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete storageclasses.storage.k8s.io custom2
  [ $status -eq 1 ]
}


@test "Delete storageClass with List and Delete operations" {
  kubectl patch tenants.capsule.clastix.io storageclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "StorageClasses", "operations": ["List", "Delete"]}]}]'
  kubectl patch tenants.capsule.clastix.io storageclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/1/proxySettings","value":[{"kind": "StorageClasses", "operations": ["List", "Delete"]}]}]'
  kubectl patch tenants.capsule.clastix.io storageclassuser --type=json -p '[{"op": "add", "path": "/spec/owners/2/proxySettings","value":[{"kind": "StorageClasses", "operations": ["List", "Delete"]}]}]'
  kubectl patch tenants.capsule.clastix.io storageclassgroup --type=json -p '[{"op": "add", "path": "/spec/owners/0/proxySettings","value":[{"kind": "StorageClasses", "operations": ["List", "Delete"]}]}]'

  echo "Delete allowed storageClass" >&3
  poll_until_equals "User" 'storageclass.storage.k8s.io "custom" deleted' "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig delete storageclasses.storage.k8s.io custom" 3 5
  poll_until_equals "SA" 'storageclass.storage.k8s.io "cephfs" deleted' "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig delete storageclasses.storage.k8s.io cephfs" 3 5
  poll_until_equals "Group - storageClass 1" 'storageclass.storage.k8s.io "custom2" deleted' "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete storageclasses.storage.k8s.io custom2" 3 5
  poll_until_equals "Group - storageClass 2" 'storageclass.storage.k8s.io "glusterfs" deleted' "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete storageclasses.storage.k8s.io glusterfs" 3 5

  echo "Delete nonallowed storageClass" >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig delete storageclasses.storage.k8s.io nonallowed
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig delete storageclasses.storage.k8s.io nonallowed
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete storageclasses.storage.k8s.io nonallowed
  [ $status -eq 1 ]
}
