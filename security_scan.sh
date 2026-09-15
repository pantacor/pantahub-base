#!/bin/sh
# Run the Go vulnerability and static security scans.
set -eu

# Pinned, not @latest: @latest follows upstream, so a tool release that raises
# its own Go requirement breaks this scan on a day nobody touched the repo --
# govulncheck v1.8.0 needs Go >= 1.26 and did exactly that while the scan still
# ran on 1.25. These belong to the Go release in Dockerfile.security-scan; bump
# them together with it.
GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.8.0}"
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

# gosec was installed here but never invoked, so the SARIF report CI uploads as
# its SAST artifact was always the empty file the job touched. Run it.
#
# The report is written before the exit status is acted on, so a failing scan
# still leaves an artifact to read.
set +e
gosec -fmt sarif -out gosec.sarif ./... >gosec.out 2>&1
gosec_status=$?
set -e
cat gosec.out

if [ "$gosec_status" -ne 0 ]; then
    echo "gosec findings by rule:"
    grep -o '"ruleId": *"[^"]*"' gosec.sarif |
        sed 's/.*: *"//;s/"$//' |
        sort | uniq -c | sort -rn
    echo
    echo "Build and deploy are gated on this: nothing ships while gosec reports."
    echo "Full detail is in the SAST artifact (gosec.sarif)."
    exit "$gosec_status"
fi
