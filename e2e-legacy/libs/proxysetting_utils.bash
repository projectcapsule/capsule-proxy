#!/usr/bin/env bash

function create_proxysetting() {
  local name namespace owner ownerKind
  name=${1}
  namespace=${2}
  owner=${3}
  ownerKind=${4}

  cat <<EOF | kubectl 3>&- apply -f -
apiVersion: capsule.clastix.io/v1beta1
kind: ProxySetting
metadata:
  name: ${name}
  namespace: ${namespace}
spec:
  subjects:
  - name: ${owner}
    kind: ${ownerKind}
EOF

}

function delete_proxysetting() {
  local name namespace
  name=${1}
  namespace=${2}

  kubectl delete proxysettings.capsule.clastix.io "${name}" -n "${namespace}"
}
