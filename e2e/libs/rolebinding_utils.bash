#!/usr/bin/env bash

function create_rolebinding() {
  local username namespace
  name=${1}
  kind=${2}
  namespace=${3}

  cat <<EOF | kubectl apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: ${name}
  namespace: ${namespace}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: admin
subjects:
  - apiGroup: rbac.authorization.k8s.io
    kind: ${kind}
    name: ${name}
EOF
}
