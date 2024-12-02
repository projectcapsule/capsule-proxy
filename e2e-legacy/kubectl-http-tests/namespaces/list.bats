#!/usr/bin/env bats

load "$BATS_TEST_DIRNAME/../../libs/poll.bash"

@test "List namespaces" {
  namespaces=$(kubectl get ns -o name)
  poll_until_different "User" "$namespaces" "kubectl --kubeconfig=${HACK_DIR}/alice.kubeconfig get namespaces --output=name" 3 5
}
