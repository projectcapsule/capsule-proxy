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

@test "Update ingressClass without permissions" {
  if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -lt 18 ]]; then
    kubectl version
    skip "IngressClass resources is not supported on Kubernetes < 1.18"
  fi

  echo "Update ingressClass without List and Update operations" >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig label ingressclasses.networking.k8s.io custom capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig label ingressclasses.networking.k8s.io external-lb capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig label ingressclasses.networking.k8s.io custom2 capsule.clastix.io/test=labeling
  [ $status -eq 1 ]

  echo "Update ingressClass with only List operation" >&3
  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/0/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List"]}]}]'
  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/1/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List"]}]}]'
  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/2/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List"]}]}]'
  kubectl patch proxysettings.capsule.clastix.io ingressclassgroup -n ingressclassgroup-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/0/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List"]}]}]'

  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig label ingressclasses.networking.k8s.io custom capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig label ingressclasses.networking.k8s.io external-lb capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig label ingressclasses.networking.k8s.io custom2 capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
}

@test "Update ingressClass with List and Update operations" {
  if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -lt 18 ]]; then
    kubectl version
    skip "IngressClass resources is not supported on Kubernetes < 1.18"
  fi

  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/0/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List", "Update"]}]}]'
  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/1/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List", "Update"]}]}]'
  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/2/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List", "Update"]}]}]'
  kubectl patch proxysettings.capsule.clastix.io ingressclassgroup -n ingressclassgroup-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/0/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List", "Update"]}]}]'

  echo "Update allowed ingressClass" >&3
  poll_until_equals "User" "ingressclass.networking.k8s.io/custom labeled" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig label ingressclasses.networking.k8s.io custom capsule.clastix.io/test=labeling" 3 5
  poll_until_equals "SA" "ingressclass.networking.k8s.io/external-lb labeled" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig label ingressclasses.networking.k8s.io external-lb capsule.clastix.io/test=labeling" 3 5
  poll_until_equals "Group - storageClass 1" "ingressclass.networking.k8s.io/custom2 labeled" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig label ingressclasses.networking.k8s.io custom2 capsule.clastix.io/test=labeling" 3 5
  poll_until_equals "Group - storageClass 2" "ingressclass.networking.k8s.io/internal-lb labeled" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig label ingressclasses.networking.k8s.io internal-lb capsule.clastix.io/test=labeling" 3 5

  echo "Update nonallowed ingressClass" >&3
  run kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig label ingressclasses.networking.k8s.io nonallowed capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig label ingressclasses.networking.k8s.io nonallowed capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
  run kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig label ingressclasses.networking.k8s.io nonallowed capsule.clastix.io/test=labeling
  [ $status -eq 1 ]
}
