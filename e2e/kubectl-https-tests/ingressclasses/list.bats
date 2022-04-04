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

@test "List ingressClasses without permissions" {
  if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -lt 18 ]]; then
    kubectl version
    skip "IngressClass resources is not supported on Kubernetes < 1.18"
  fi

  poll_until_equals "User" "" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get ingressclasses.networking.k8s.io --output=name" 3 5
  poll_until_equals "SA" "" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get ingressclasses.networking.k8s.io --output=name" 3 5
  poll_until_equals "Group" "" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get ingressclasses.networking.k8s.io --output=name" 3 5
}

@test "List ingressClasses with List operation" {
  if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -lt 18 ]]; then
    kubectl version
    skip "IngressClass resources is not supported on Kubernetes < 1.18"
  fi

  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/0/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List"]}]}]'
  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/1/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List"]}]}]'
  kubectl patch proxysettings.capsule.clastix.io ingressclassuser -n ingressclassuser-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/2/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List"]}]}]'
  kubectl patch proxysettings.capsule.clastix.io ingressclassgroup -n ingressclassgroup-namespace --type=json -p '[{"op": "add", "path": "/spec/subjects/0/proxySettings","value":[{"kind": "IngressClasses", "operations": ["List"]}]}]'

  local userlist="ingressclass.networking.k8s.io/custom
ingressclass.networking.k8s.io/external-lb
ingressclass.networking.k8s.io/internal-lb"

  local grouplist="ingressclass.networking.k8s.io/custom
ingressclass.networking.k8s.io/custom2
ingressclass.networking.k8s.io/external-lb
ingressclass.networking.k8s.io/internal-lb"

  poll_until_equals "User" "$userlist" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get ingressclasses.networking.k8s.io --output=name" 3 5
  poll_until_equals "SA" "$userlist" "kubectl --kubeconfig=${HACK_DIR}/sa.kubeconfig get ingressclasses.networking.k8s.io --output=name" 3 5
  poll_until_equals "Group" "$grouplist" "kubectl --kubeconfig=${HACK_DIR}/foo.clastix.io.kubeconfig get ingressclasses.networking.k8s.io --output=name" 3 5
}
