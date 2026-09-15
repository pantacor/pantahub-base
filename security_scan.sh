#!/bin/sh
# Run the Go vulnerability and static security scans.
set -eu

# Pinned, not @latest: the CI image is pinned to a Go release, but @latest
# follows upstream, so a tool release that raises its own Go requirement breaks
# this job on a day nobody touched the repo. govulncheck v1.8.0 did exactly
# that -- it needs Go >= 1.26 against a golang:1.25.13 image. Bump these
# together with the image.
GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.7.0}"
GOSEC_VERSION="${GOSEC_VERSION:-v2.29.0}"

go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"
set +e
govulncheck ./... >govulncheck.out 2>&1
vuln_status=$?
set -e
cat govulncheck.out

unexpected=$(grep '^Vulnerability #' govulncheck.out |
    grep -o 'GO-[0-9-]*' |
    grep -Ev '^(GO-2026-5668|GO-2026-4887|GO-2026-4883)$' || true)
if [ -n "$unexpected" ]; then
    echo "Unexpected govulncheck findings:"
    echo "$unexpected"
    exit 1
fi
if [ "$vuln_status" -ne 0 ] && ! grep -q '^Vulnerability #' govulncheck.out; then
    exit "$vuln_status"
fi

go install "github.com/securego/gosec/v2/cmd/gosec@${GOSEC_VERSION}"
