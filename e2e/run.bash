#!/usr/bin/env bash
set -eo pipefail

echo ">>> Waiting for capsule-proxy pod to be ready for accepting requests"
kubectl --namespace capsule-system wait --for=condition=ready --timeout=320s pod -l app.kubernetes.io/instance=capsule-proxy
echo ">>> Waiting for capsule pod to be ready for accepting requests"
kubectl --namespace capsule-system wait --for=condition=ready --timeout=320s pod -l app.kubernetes.io/instance=capsule

echo ">>> Starting test suite"
bats -t "$(git rev-parse --show-toplevel)"/e2e/tests/*
