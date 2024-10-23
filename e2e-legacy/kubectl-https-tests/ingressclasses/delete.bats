#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../../libs/namespaces_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/ingressclass_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/serviceaccount_utils.bash"
load "$BATS_TEST_DIRNAME/../../libs/proxysetting_utils.bash"

setup() {
  create_tenant ingressclassuser alice User
  kubectl patch tenants.capsule.clastix.io ingressclassuser --type=json -p '[{"op": "add", "path": "/spec/ingressOptions", "value": {"allowedClasses": {"allowed": ["custom"], "allowedRegex": "\\w+-lb"}}}]'
  create_namespace alice ingressclassuser-namespace
  create_serviceaccount sa ingressclassuser-namespace
  create_proxysetting ingressclassuser ingressclassuser-namespace alice User
  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/1","value":{"kind": "ServiceAccount", "name": "system:serviceaccount:ingressclassuser-namespace:sa"}}]'
  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/2","value":{"kind": "Group", "name": "foo.clastix.io"}}]'

  create_tenant ingressclassgroup foo.clastix.io Group
  kubectl patch tenants.capsule.clastix.io ingressclassgroup --type=json -p '[{"op": "add", "path": "/spec/ingressOptions", "value": {"allowedClasses": {"allowed": ["custom2"]}}}]'
  create_namespace joe ingressclassgroup-namespace foo.clastix.io
  create_proxysetting ingressclassgroup ingressclassgroup-namespace foo.clastix.io Group

  if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -gt 17 ]]; then
    local version="v1"
    if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -lt 19 ]]; then
      version="v1beta1"
    fi
    create_ingressclass "${version}" custom
    create_ingressclass "${version}" custom2
    create_ingressclass "${version}" external-lb
    create_ingressclass "${version}" internal-lb
    create_ingressclass "${version}" nonallowed
  fi
}

teardown() {
  delete_tenant ingressclassuser
  delete_tenant ingressclassgroup

  delete_ingressclass custom || true
  delete_ingressclass custom2 || true
  delete_ingressclass external-lb || true
  delete_ingressclass internal-lb || true
  delete_ingressclass nonallowed || true
}

@test "Delete ingressClass without permissions" {
  if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -lt 18 ]]; then
    kubectl version
    skip "IngressClass resources is not supported on Kubernetes < 1.18"
  fi

  echo "Delete ingressClass without List and Delete operations" >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig delete ingressclasses.networking.k8s.io external-lb
  [ "$status" -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig delete ingressclasses.networking.k8s.io cephfs
  [ "$status" -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete ingressclasses.networking.k8s.io custom2
  [ "$status" -eq 1 ]

  echo "Delete ingressClass with only List operation" >&3
  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/0/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List"]}]}]'
  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/1/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List"]}]}]'
  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/2/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List"]}]}]'
  kubectl patch proxysettings.capsule.clastix.io ingressclassgroup -n ingressclassgroup-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/0/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List"]}]}]'

  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig delete ingressclasses.networking.k8s.io external-lb
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig delete ingressclasses.networking.k8s.io cephfs
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete ingressclasses.networking.k8s.io custom2
  [ $status -eq 1 ]
}

@test "Delete ingressClass with List and Delete operations" {
  if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -lt 18 ]]; then
    kubectl version
    skip "IngressClass resources is not supported on Kubernetes < 1.18"
  fi

  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/0/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List", "Delete"]}]}]'
  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/1/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List", "Delete"]}]}]'
  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/2/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List", "Delete"]}]}]'
  kubectl patch proxysettings.capsule.clastix.io ingressclassgroup -n ingressclassgroup-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/0/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List", "Delete"]}]}]'

  echo "Delete allowed ingressClass" >&3
  poll_until_equals "User" 'ingressclass.networking.k8s.io "custom" deleted' "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig delete ingressclasses.networking.k8s.io custom" 3 5
  poll_until_equals "SA" 'ingressclass.networking.k8s.io "external-lb" deleted' "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig delete ingressclasses.networking.k8s.io external-lb" 3 5
  poll_until_equals "Group - storageClass 1" 'ingressclass.networking.k8s.io "custom2" deleted' "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete ingressclasses.networking.k8s.io custom2" 3 5
  poll_until_equals "Group - storageClass 2" 'ingressclass.networking.k8s.io "internal-lb" deleted' "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete ingressclasses.networking.k8s.io internal-lb" 3 5

  echo "Delete nonallowed ingressClass" >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig delete ingressclasses.networking.k8s.io nonallowed
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig delete ingressclasses.networking.k8s.io nonallowed
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig delete ingressclasses.networking.k8s.io nonallowed
  [ $status -eq 1 ]
}
