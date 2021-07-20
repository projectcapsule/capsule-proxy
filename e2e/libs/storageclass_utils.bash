#!/usr/bin/env bash

function create_storageclass() {
  local name
  name=${1}
  cat <<EOF | kubectl apply -f -
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  labels:
    name: ${name}
  name: ${name}
provisioner: capsule.clastix.io/${name}
reclaimPolicy: Delete
volumeBindingMode: WaitForFirstConsumer
EOF
}

function delete_storageclass() {
  local name
  name=${1}

  kubectl delete storageclasses.storage.k8s.io "${name}"
}
