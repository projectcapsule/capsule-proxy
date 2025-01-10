#!/usr/bin/env bash
set -eo pipefail

TESTS=$1

HACK_DIR="$(git rev-parse --show-toplevel)/hack"
export HACK_DIR

echo ">>> Waiting for capsule-proxy pod to be ready for accepting requests"
kubectl --namespace capsule-system wait --for=condition=ready --timeout=320s pod -l app.kubernetes.io/name=capsule-proxy

echo ">>> Waiting for capsule pod to be ready for accepting requests"
kubectl --namespace capsule-system wait --for=condition=ready --timeout=320s pod -l app.kubernetes.io/instance=capsule

echo ">>> Waiting for metrics-server pod to be ready for listing metrics"
kubectl --namespace metrics-system wait --for=condition=ready --timeout=320s pod -l app.kubernetes.io/instance=metrics-server
until [[ $(kubectl get --raw "/apis/metrics.k8s.io/v1beta1/nodes" 2>/dev/null | jq type 2>/dev/null) == "\"object\"" ]]
do 
    printf "."
    sleep 5
done
until [[ $(kubectl get --raw "/apis/metrics.k8s.io/v1beta1/nodes" 2>/dev/null | jq '.items' 2>/dev/null | jq length 2>/dev/null) == $(kubectl get nodes -o=name | wc -l) ]]
do 
    printf "."
    sleep 5
done

echo -e "\n>>> Starting test suite ${TESTS}"
bats -t "$(git rev-parse --show-toplevel)"/e2e/${TESTS}-tests/*
