#!/usr/bin/env bash

function create_ingressclass() {
  local version name
  version=${1}
  name=${2}
  cat <<EOF | kubectl apply -f -
apiVersion: networking.k8s.io/${version}
kind: IngressClass
metadata:
  name: ${name}
  labels:
    name: ${name}
spec:
  controller: example.com/ingress-controller
EOF
}

function delete_ingressclass() {
  local name
  name=${1}

  kubectl delete ingressclasses.networking.k8s.io "${name}"
}
