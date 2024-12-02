#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../libs/poll.bash"

@test "Checking kubectl api-resources" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl api-resources"
  [ $status -ne 0 ]
}

@test "Checking kubectl version" {
  run sh -c "KUBECONFIG=${HACK_DIR}/alice.kubeconfig kubectl version"
  [ $status -ne 0 ]
}
