#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../libs/tenants_utils.bash"
load "$BATS_TEST_DIRNAME/../libs/poll.bash"
load "$BATS_TEST_DIRNAME/../libs/namespaces_utils.bash"
load "$BATS_TEST_DIRNAME/../libs/ingressclass_utils.bash"

setup() {
  create_tenant ingressclass alice
  kubectl patch tenants.capsule.clastix.io ingressclass --type=json -p '[{"op": "add", "path": "/spec/ingressClasses", "value": {"allowed": ["custom"], "allowedRegex": "\\w+-lb"}}]'

  if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -gt 17 ]]; then
    local version="v1"
    if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -lt 19 ]]; then
      version="v1beta1"
    fi
    create_ingressclass "${version}" custom
    create_ingressclass "${version}" external-lb
    create_ingressclass "${version}" internal-lb
    create_ingressclass "${version}" nonallowed
  fi
}

teardown() {
  delete_tenant ingressclass

  delete_ingressclass custom || true
  delete_ingressclass external-lb || true
  delete_ingressclass internal-lb || true
  delete_ingressclass nonallowed || true
}

@test "Ingress Class retrieval via kubectl" {
  if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -lt 18 ]]; then
    kubectl version
    skip "IngressClass resources is not suported on Kubernetes < 1.18"
  fi

  poll_until_equals "no IngressClass" "" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get ingressclasses.networking.k8s.io --output=name" 3 5

  kubectl patch tenants.capsule.clastix.io ingressclass --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-ingressclass-listing": "true"}}]'

  local list="ingressclass.networking.k8s.io/custom
ingressclass.networking.k8s.io/external-lb
ingressclass.networking.k8s.io/internal-lb"
  poll_until_equals "IngressClass retrieval" "$list" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get ingressclasses.networking.k8s.io --output=name" 3 5
}

@test "Getting Non-allowed IngressClass is denied" {
  if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -lt 18 ]]; then
    kubectl version
    skip "IngressClass resources is not suported on Kubernetes < 1.18"
  fi

  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl get ingressclasses.networking.k8s.io nonallowed"
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (NotFound): ingressclasses.networking.k8s.io "nonallowed" not found' ]
}

@test "Patching IngressClass via kubectl" {
  if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -lt 18 ]]; then
    kubectl version
    skip "IngressClass resources is not suported on Kubernetes < 1.18"
  fi

  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl label ingressclasses.networking.k8s.io custom capsule.clastix.io/test=labeling"
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (NotFound): ingressclasses.networking.k8s.io "custom" not found' ]

  kubectl patch tenants.capsule.clastix.io ingressclass --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-ingressclass-listing": "true"}}]'
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl label ingressclasses.networking.k8s.io custom capsule.clastix.io/test=labeling"
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): ingressclasses.networking.k8s.io "custom" is forbidden: User "alice" cannot patch resource "ingressclasses" in API group "networking.k8s.io" at the cluster scope' ]

  kubectl patch tenants.capsule.clastix.io ingressclass --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-ingressclass-listing": "true", "capsule.clastix.io/enable-ingressclass-update": "true"}}]'
  poll_until_equals "ingressClass patching" "ingressclass.networking.k8s.io/custom labeled" "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl label ingressclasses.networking.k8s.io custom capsule.clastix.io/test=labeling" 3 5
}

@test "Deleting IngressClass via kubectl" {
  if [[ $(kubectl version -o json | jq -r .serverVersion.minor) -lt 18 ]]; then
    kubectl version
    skip "IngressClass resources is not suported on Kubernetes < 1.18"
  fi

  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl delete ingressclasses.networking.k8s.io custom"
  [ $status -eq 1 ]
  [ "${lines[0]}" = 'Error from server (Forbidden): ingressclasses.networking.k8s.io "custom" is forbidden: User "alice" cannot delete resource "ingressclasses" in API group "networking.k8s.io" at the cluster scope' ]

  kubectl patch tenants.capsule.clastix.io ingressclass --type=json -p '[{"op": "add", "path": "/metadata/annotations", "value": {"capsule.clastix.io/enable-ingressclass-listing": "true", "capsule.clastix.io/enable-ingressclass-deletion": "true"}}]'
  poll_until_equals "ingressclass deletion" 'ingressclass.networking.k8s.io "custom" deleted' "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl delete ingressclasses.networking.k8s.io custom" 3 5
}
