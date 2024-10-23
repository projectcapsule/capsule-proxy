#!/usr/bin/env bash

function create_clusterrolebinding() {
  local name namespace
  name=${1}
  namespace=${2}

  cat <<EOF | kubectl apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ${namespace}:${name}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: capsule-apis
subjects:
- kind: ServiceAccount
  name: ${name}
  namespace: ${namespace}
EOF
}

function delete_clusterrolebinding() {
  local name namespace
  name=${1}
  namespace=${2}

  kubectl delete clusterrolebinding ${namespace}:${name}
}


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

  create_clusterrolebinding ${name} ${namespace}

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
