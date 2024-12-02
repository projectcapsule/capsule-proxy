#!/usr/bin/env bash

function create_tenant() {
  local name owner ownerKind
  name=${1}
  owner=${2}
  ownerKind=${3}

  cat <<EOF | kubectl 3>&- apply -f -
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: ${name}
spec:
  owners:
    - kind: ${ownerKind}
      name: ${owner}
EOF

}

function delete_tenant() {
  local name
  name=${1}

  kubectl get tenants.capsule.clastix.io "${name}" -o=jsonpath='{.status.namespaces}' | jq ".[]" | xargs -L1 -I'{}' kubectl delete namespace {}
  kubectl delete tenants.capsule.clastix.io "${name}"
}
