#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../../libs/namespaces_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/serviceaccount_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/rolebinding_utils.bash"

setup() {
  create_tenant oil alice User
  kubectl patch tenants.capsule.clastix.io oil --type=json -p '[{"op": "add", "path": "/spec/owners/1", "value": {"kind": "ServiceAccount", "name": "system:serviceaccount:oil-dev:sa"}}]'
  kubectl patch tenants.capsule.clastix.io oil --type=json -p '[{"op": "add", "path": "/spec/owners/2", "value": {"kind": "Group", "name": "foo.clastix.io"}}]'
  kubectl patch tenants.capsule.clastix.io oil --type=json -p '[{"op": "add", "path": "/spec/namespaceOptions", "value": {"additionalMetadata": {"labels": {"bizz": "buzz"}}}}]'
  create_namespace alice oil-dev
  create_namespace alice oil-staging
  create_namespace alice oil-production
  create_serviceaccount sa oil-dev

  create_tenant metal alice User
  kubectl patch tenants.capsule.clastix.io metal --type=json -p '[{"op": "add", "path": "/spec/namespaceOptions", "value": {"additionalMetadata": {"labels": {"foo": "bar"}}}}]'
  create_namespace alice metal-staging

  create_tenant gas foo.clastix.io Group
  kubectl patch tenants.capsule.clastix.io gas --type=json -p '[{"op": "add", "path": "/spec/namespaceOptions", "value": {"additionalMetadata": {"labels": {"bizz": "buzz"}}}}]'
  create_namespace joe gas-qa foo.clastix.io

  create_rolebinding dave User metal-staging
  create_rolebinding dave User gas-qa
}

teardown() {
  delete_tenant oil
  delete_tenant gas
  delete_tenant metal
}

@test "List allowed namespaces" {
  echo "Without label selector" >&3
  local user="namespace/metal-staging
namespace/oil-dev
namespace/oil-production
namespace/oil-staging"
  local sa="namespace/oil-dev
namespace/oil-production
namespace/oil-staging"
  local group="namespace/gas-qa
namespace/oil-dev
namespace/oil-production
namespace/oil-staging"
  local rolebindings="namespace/gas-qa
namespace/metal-staging"

  poll_until_equals "User" "$user" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get namespaces --output=name" 3 5
  poll_until_equals "SA" "$sa" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get namespaces --output=name" 3 5
  poll_until_equals "Group" "$group" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get namespaces --output=name" 3 5
  poll_until_equals "Rolebindings" "$rolebindings" "kubectl --kubeconfig=${HACK_DIR}/dave.kubeconfig get namespaces --output=name" 3 5

  echo "With label selector" >&3
  local user_filtered="namespace/metal-staging"
  local group_filtered="namespace/gas-qa
namespace/oil-dev
namespace/oil-production
namespace/oil-staging"
  local rolebindings_filtered="namespace/metal-staging"
  poll_until_equals "User" "$user_filtered" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get namespaces --output=name --selector foo=bar" 3 5
  poll_until_equals "SA" "" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get namespaces --output=name --selector foo=bar" 3 5
  poll_until_equals "Group" "$group_filtered" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get namespaces --output=name --selector bizz=buzz" 3 5
  poll_until_equals "Rolebindings" "$rolebindings_filtered" "kubectl --kubeconfig=${HACK_DIR}/dave.kubeconfig get namespaces --output=name --selector foo=bar" 3 5
}

@test "List objects in nonallowed namespaces" {
  echo "User" >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get namespace default
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (NotFound): namespace "default" not found' ]
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig --namespace default get pods
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): pods is forbidden: User "alice" cannot list resource "pods" in API group "" in the namespace "default"' ]
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig --namespace default get services
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): services is forbidden: User "alice" cannot list resource "services" in API group "" in the namespace "default"' ]
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig --namespace default get configmaps
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): configmaps is forbidden: User "alice" cannot list resource "configmaps" in API group "" in the namespace "default"' ]

  echo "SA" >&3
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get namespace default
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): namespaces "default" is forbidden: User "system:serviceaccount:oil-dev:sa" cannot get resource "namespaces" in API group "" in the namespace "default"' ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig --namespace default get pods
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): pods is forbidden: User "system:serviceaccount:oil-dev:sa" cannot list resource "pods" in API group "" in the namespace "default"' ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig --namespace default get services
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): services is forbidden: User "system:serviceaccount:oil-dev:sa" cannot list resource "services" in API group "" in the namespace "default"' ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig --namespace default get configmaps
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): configmaps is forbidden: User "system:serviceaccount:oil-dev:sa" cannot list resource "configmaps" in API group "" in the namespace "default"' ]

  echo "Group" >&3
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get namespace default
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): namespaces "default" is forbidden: User "joe" cannot get resource "namespaces" in API group "" in the namespace "default"' ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig --namespace default get pods
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): pods is forbidden: User "joe" cannot list resource "pods" in API group "" in the namespace "default"' ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig --namespace default get services
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): services is forbidden: User "joe" cannot list resource "services" in API group "" in the namespace "default"' ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig --namespace default get configmaps
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): configmaps is forbidden: User "joe" cannot list resource "configmaps" in API group "" in the namespace "default"' ]

   echo "RoleBindings" >&3
   run kubectl --kubeconfig=${HACK_DIR}/dave.kubeconfig get namespace default
   [ $status -eq 1 ]
   [ "${lines[0]}" = 'Error from server (Forbidden): namespaces "default" is forbidden: User "dave" cannot get resource "namespaces" in API group "" in the namespace "default"' ]
   run kubectl --kubeconfig=${HACK_DIR}/dave.kubeconfig --namespace default get pods
   [ $status -eq 1 ]
   [ "${lines[0]}" = 'Error from server (Forbidden): pods is forbidden: User "dave" cannot list resource "pods" in API group "" in the namespace "default"' ]
   run kubectl --kubeconfig=${HACK_DIR}/dave.kubeconfig --namespace default get services
   [ $status -eq 1 ]
   [ "${lines[0]}" = 'Error from server (Forbidden): services is forbidden: User "dave" cannot list resource "services" in API group "" in the namespace "default"' ]
   run kubectl --kubeconfig=${HACK_DIR}/dave.kubeconfig --namespace default get configmaps
   [ $status -eq 1 ]
   [ "${lines[0]}" = 'Error from server (Forbidden): configmaps is forbidden: User "dave" cannot list resource "configmaps" in API group "" in the namespace "default"' ]
}
