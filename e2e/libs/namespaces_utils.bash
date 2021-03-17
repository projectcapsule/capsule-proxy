#!/usr/bin/env bash

function create_namespace() {
  local user name
  user=${1}
  name=${2}

  kubectl --as="${user}" --as-group="capsule.clastix.io" create namespace "${name}"
  sleep 1
}
