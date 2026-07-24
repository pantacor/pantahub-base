#!/usr/bin/env bash
# Smoke test for the pantahub chart.
#
# Spins up a throwaway k3d cluster, installs the chart, waits for the stack
# to become available, then probes the client-facing endpoints from inside
# the cluster. The cluster is deleted on exit (KEEP=true to keep it).
#
# Requirements: docker, k3d, kubectl, helm — on an amd64 host (the pantacor
# and confluent images ship amd64-only; on arm64 use VALUES to disable them,
# which reduces the test to mongo/elasticsearch/localstack).
#
# Usage:
#   ./smoke-test.sh                       # full run
#   VALUES=my-values.yaml ./smoke-test.sh # extra values overlay
#   KEEP=true ./smoke-test.sh             # keep the cluster for inspection
set -euo pipefail

CLUSTER=${CLUSTER:-pantahub-smoke}
NAMESPACE=${NAMESPACE:-pantahub}
CHART_DIR=$(cd "$(dirname "$0")" && pwd)
VALUES=${VALUES:-}
KEEP=${KEEP:-false}
TIMEOUT=${TIMEOUT:-900s}

if [ "$(uname -m)" != "x86_64" ]; then
    echo "WARNING: non-amd64 host — pantacor/confluent images will not run here" >&2
fi

cleanup() {
    if [ "$KEEP" != "true" ]; then
        k3d cluster delete "$CLUSTER" >/dev/null 2>&1 || true
    else
        echo "Cluster kept. Use: export KUBECONFIG=\$(k3d kubeconfig write $CLUSTER)"
    fi
}
trap cleanup EXIT

k3d cluster create "$CLUSTER" --wait --timeout 180s \
    --kubeconfig-update-default=false --kubeconfig-switch-context=false
KUBECONFIG=$(k3d kubeconfig write "$CLUSTER")
export KUBECONFIG

helm install pantahub "$CHART_DIR" -n "$NAMESPACE" --create-namespace \
    ${VALUES:+-f "$VALUES"}

echo "Waiting for deployments (includes image pulls; first run is slow)..."
kubectl -n "$NAMESPACE" wait --for=condition=available deploy --all --timeout="$TIMEOUT"

FAILED=0

# connection-level probe: passes on any HTTP response, fails if unreachable
probe() {
    local name=$1 url=$2
    if kubectl -n "$NAMESPACE" run "smoke-$name" --rm -i --restart=Never \
        --image=curlimages/curl:8.11.1 -- curl -s -o /dev/null -m 10 "$url" >/dev/null 2>&1; then
        echo "PASS  $name  ($url)"
    else
        echo "FAIL  $name  ($url)"
        FAILED=1
    fi
}

deploy_enabled() {
    kubectl -n "$NAMESPACE" get deploy "$1" >/dev/null 2>&1
}

deploy_enabled base && probe base http://base:12365/
deploy_enabled www && probe www http://www:80/
deploy_enabled pvr && probe pvr http://pvr:12367/
deploy_enabled gc && probe gc http://gc:2000/
deploy_enabled elasticsearch && probe elasticsearch http://elasticsearch:9200/_cluster/health

# the mongo replica set must actually have initialized
if deploy_enabled mongo; then
    if kubectl -n "$NAMESPACE" exec deploy/mongo -- \
        mongo --quiet --eval 'rs.status().ok' 2>/dev/null | grep -q 1; then
        echo "PASS  mongo replica set rs0"
    else
        echo "FAIL  mongo replica set rs0"
        FAILED=1
    fi
fi

if [ "$FAILED" = 0 ]; then
    echo "SMOKE TEST PASSED"
else
    echo "SMOKE TEST FAILED"
    exit 1
fi
