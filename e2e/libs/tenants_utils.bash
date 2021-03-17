#!/usr/bin/env bash

function create_tenant() {
  local name owner
  name=${1}
  owner=${2}

  cat <<EOF | kubectl apply -f -
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: ${name}
spec:
  owner:
    kind: User
    name: ${owner}
EOF

  sleep 1
}

function delete_tenant() {
  local name
  name=${1}

  kubectl get tenants.capsule.clastix.io "${name}" -o=jsonpath='{.status.namespaces}' | jq ".[]" | xargs -L1 -I'{}' kubectl delete namespace {}
  kubectl delete tenants.capsule.clastix.io "${name}"

  sleep 1
}
