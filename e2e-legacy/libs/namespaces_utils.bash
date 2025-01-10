#!/usr/bin/env bash

function create_namespace() {
  local user name group
  user=${1}
  name=${2}
  group=${3}


  if [ -z "$group" ]
  then
    kubectl --as="${user}" --as-group="capsule.clastix.io" create namespace "${name}"
  else
    kubectl --as="${user}" --as-group="capsule.clastix.io" --as-group="${group}" create namespace "${name}"
  fi
}
