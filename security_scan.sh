#!/bin/sh
# Run the Go vulnerability and static security scans.
set -eu

go install golang.org/x/vuln/cmd/govulncheck@latest
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

go install github.com/securego/gosec/v2/cmd/gosec@latest
