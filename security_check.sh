#!/bin/sh
# Run the same Go security checks as CI inside Docker.
set -eu

IMAGE=pantahub-base-security-scan

docker build -f Dockerfile.security-scan -t "$IMAGE" .
# Docker advisories GO-2026-5668, GO-2026-4887, and GO-2026-4883 are
# currently unfixed upstream and required for PVR local-image support.
docker run --rm \
    -v "$(pwd)":/src \
    -w /src \
    -v pantahub-base-gocache:/root/.cache/go-build \
    -v pantahub-base-gomod:/go/pkg/mod \
    "$IMAGE"
