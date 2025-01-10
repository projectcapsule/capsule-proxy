#!/usr/bin/env bats

# Thanks FluxCD!
# source code: https://github.com/fluxcd/flux/blob/fa23a359726c70177b78128b7318926adf206e2c/test/e2e/lib/poll.bash

function poll_until_equals() {
  local what="$1"; shift
  local expected="$1"; shift
  local check_cmd="$1"; shift
  local retries="$1"; shift
  local wait_period="$1"
  poll_until_true "$what" "[ '$expected' = \"\$( $check_cmd )\" ]" "$retries" "$wait_period"
}

function poll_until_different() {
  local what="$1"; shift
  local expected="$1"; shift
  local check_cmd="$1"; shift
  local retries="$1"; shift
  local wait_period="$1"
  poll_until_true "$what" "[ '$expected' != \"\$( $check_cmd )\" ]" "$retries" "$wait_period"
}

function poll_until_true() {
  local what="$1"
  local check_cmd="$2"
  # timeout after $retries * $wait_period seconds
  local retries=${3:-24}
  local wait_period=${4:-5}
  echo -n ">>> Waiting for $what " >&3
  local count=0
  until eval "$check_cmd"; do
    echo -n '.' >&3
    sleep "$wait_period" 3>&-
    count=$((count + 1))
    if [[ ${count} -eq ${retries} ]]; then
      echo ': no more retries left!' >&3
      return 1 # fail
    fi
  done
  echo ': done' >&3
}
