#!/usr/bin/env bash

function create_priorityclass() {
  local name
  name=${1}
  cat <<EOF | kubectl apply -f -
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  labels:
    name: ${name}
  name: ${name}
value: 1000
globalDefault: false
description: "Testing priority class for ${name} class"
EOF
}

function delete_priorityclass() {
  local name
  name=${1}

  kubectl delete priorityclasses.scheduling.k8s.io "${name}"
}
