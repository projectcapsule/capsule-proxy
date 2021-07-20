#!/usr/bin/env bash

function create_serviceaccount() {
  local name namespace
  name=${1}
  namespace=${2}


  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ${name}
  namespace: ${namespace}
EOF


  SECRETNAME=$(kubectl get serviceaccounts -n ${namespace} ${name}  -o jsonpath='{.secrets[0].name}')
  TOKEN=$(kubectl get secret -n ${namespace} ${SECRETNAME} -o jsonpath='{.data.token}' | base64 -d)

  cat > hack/sa.kubeconfig <<EOF
apiVersion: v1
clusters:
- cluster:
    server: https://127.0.0.1:9001
  name: kind-capsule
contexts:
- context:
    cluster: kind-capsule
    user: sa
  name: sa
current-context: sa
kind: Config
preferences: {}
users:
- name: ${name}
  user:
    token: ${TOKEN}
EOF
}
