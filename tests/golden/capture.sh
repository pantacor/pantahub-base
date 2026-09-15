#!/usr/bin/env bash
#
# Golden-response capture for the go-json-rest -> echo migration.
#
# Records the observable HTTP contract of a running Hub API: status line, the
# headers that clients actually depend on, and the response body. Run it once
# against the CURRENT (go-json-rest) implementation to produce a baseline, then
# again against the echo build and diff the two trees. Any difference is a
# regression unless it is explicitly listed as an intended change.
#
# Auth comes from `pvr curl`, which injects pvr's bearer token, so no credential
# is ever written to disk or passed on a command line here.
#
# Usage:
#   tests/golden/capture.sh baseline            # capture to tests/golden/baseline/
#   tests/golden/capture.sh candidate           # capture to tests/golden/candidate/
#   tests/golden/capture.sh diff                # compare the two
#   API=https://api.stage.pantahub.com tests/golden/capture.sh baseline
#
# Exit status for `diff`: 0 identical, 1 differences found.

set -uo pipefail

API="${API:-https://api.stage.pantahub.com}"
UA="${UA:-pantahub-golden/0.1}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODE="${1:-}"

# Headers whose values form part of the client contract. Everything else (Date,
# Content-Length, trace ids, ...) varies per request and is deliberately ignored.
CONTRACT_HEADERS='^(www-authenticate|phjsonformat|content-type|location|allow|x-powered-by):'

# --- route table -------------------------------------------------------------
# Authenticated GETs. Extend as coverage grows; every route added here becomes
# part of the enforced contract.
AUTHED_PATHS=(
  /devices/ /trails/ /apps/ /objects/ /logs/ /dash/
  /profiles/ /subscriptions/ /tokens/ /exports/ /metrics/
)

# Paths probed WITHOUT credentials. These capture the 401 contract, which is the
# highest-risk part of the migration: pvr keys its on-disk credential store by
# the exact `WWW-Authenticate` string (ph-aeps + " realm=" + realm), so a change
# in these bytes logs out every pvr client and device in the fleet.
UNAUTH_PATHS=(
  /auth/login /devices/ /trails/ /objects/ /apps/ /logs/ /profiles/
  /subscriptions/ /tokens/ /metrics/ /healthz/ /exports/
)

# Trailing-slash pairs. Today the no-slash form is a 307 from http.ServeMux (not
# from the framework); mounting echo at "/" instead of per-prefix would silently
# remove these redirects.
SLASH_PATHS=(
  /devices /trails /apps /objects /logs /profiles /subscriptions /tokens
)

# Path-parameter routes. go-json-rest spells these `#id`, echo spells them `:id`;
# the rewrite is meant to be semantically identical, and this is what proves it.
# {DEV} is substituted with an id discovered at capture time (see discover_ids).
PARAM_PATHS=(
  "/devices/{DEV}"
  "/devices/{DEV}/user-meta"
  "/trails/{DEV}"
  "/trails/{DEV}/steps"
  "/trails/{DEV}/summary"
  "/trails/{DEV}/steps/0"
  # A revision that does not exist. This answers 500 {"code":500,"error":
  # "REST-ERR-ID-..."}, NOT 404: trails/get-trails-rev.go maps every FindOne
  # error, including mongo.ErrNoDocuments, to 500 "No access". Every device
  # polling for its next revision hits this, minting an incident id and a
  # fluentd forward on each poll. It is a latent bug -- but real Pantavisor
  # devices see this status on every poll, so changing it is a deliberate,
  # announced fix with its own device testing, never a side effect of the port.
  "/trails/{DEV}/steps/999999"
)

# Malformed identifiers. These pin the CURRENT error behaviour, which is not
# uniform: /devices/<bad> answers 400 while /trails/<bad> answers 500. The 500 is
# very likely a latent bug, but it is the contract as shipped -- if the port
# changes it, that must be a deliberate, announced fix rather than a side effect.
BAD_ID='nonexistent0000000000000000'
BADPARAM_PATHS=(
  "/devices/$BAD_ID"
  "/trails/$BAD_ID"
  "/trails/$BAD_ID/steps"
)

slug() { echo "$1" | sed 's#^/##; s#/$##; s#/#_#g; s#[^A-Za-z0-9_.-]#-#g; s#^$#root#'; }

# Endpoints whose bodies are inherently live (log streams, last-seen clocks).
# For these we pin status + contract headers only; diffing the body would produce
# a regression report on every run and train everyone to ignore it.
BODY_SKIP='^/(logs|devices|dash)/$'

# Normalise a body so that unstable values (ids, timestamps, incident ids) do not
# swamp the diff. Structure and key names are what we are pinning.
#
# REST-ERR-ID-<nanos> is minted per request by utils/resterror.go, so it must be
# masked -- but its PRESENCE is part of the contract and is asserted separately.
normalise_body() {
  local f="$1"
  if command -v jq >/dev/null 2>&1 && jq -e . >/dev/null 2>&1 < "$f"; then
    # device-meta is telemetry the DEVICE reports (freeram, load averages, disk
    # free, uptime). It changes every second and is not part of the API contract
    # we are pinning -- the contract is that the key exists and is an object. Its
    # contents would otherwise make every diff fail.
    jq -S 'walk(if type == "object" then
                  with_entries(.value =
                    if (.key | test("^(device-meta|sysinfo|storage)$"; "i"))
                    then "<VOLATILE-SUBTREE>"
                    elif (.key | test("^(id|_id|timestamp|time-?modified|time-?created|last-?seen|rev|garbage|exp|iat|tsec|tnano|dev|device|trail-touched|status-changed|meta-modified)$"; "i"))
                    then "<VAR>" else .value end)
                else . end)' < "$f" 2>/dev/null \
      | sed -E 's/REST-ERR-ID-[0-9]+/REST-ERR-ID-<VAR>/g'
  else
    sed -E 's/REST-ERR-ID-[0-9]+/REST-ERR-ID-<VAR>/g' < "$f"
  fi
}

# Record which error SHAPE an endpoint uses. The repo emits two incompatible
# shapes today -- {"Error":...} from go-json-rest and {"code","error",...} with a
# minted incident id from utils/resterror.go -- and echo's default handler would
# replace both with {"message":...}. Which shape each route uses is contract.
error_shape() {
  local f="$1"
  if   grep -q '"Error"'   "$f" 2>/dev/null; then echo 'gjr:{"Error":...}'
  elif grep -q 'REST-ERR-ID' "$f" 2>/dev/null; then echo 'rerror:{"code","error"}+incident-id'
  elif [ -s "$f" ]; then echo 'other'
  else echo 'empty'; fi
}

# Resolve the concrete ids the parameterised routes are probed with.
#
# The baseline run discovers a device id from /devices/ and records it. The
# candidate run REUSES the id the baseline recorded, so both captures address the
# same resource -- otherwise the bodies would differ for a reason that has
# nothing to do with the migration.
discover_ids() {
  local outdir="$1" idfile="$HERE/baseline/IDS"
  if [ "$outdir" != "$HERE/baseline" ] && [ -f "$idfile" ]; then
    # shellcheck disable=SC1090
    . "$idfile"; echo "reusing baseline ids: DEV=$DEV"
  else
    local tmp; tmp="$(mktemp)"
    timeout 60 pvr curl -s -o "$tmp" -A "$UA" "$API/devices/" >/dev/null 2>&1
    DEV="$(jq -r 'if type=="array" then .[0].id else (.devices[0].id // empty) end' < "$tmp" 2>/dev/null)"
    rm -f "$tmp"
    if [ -z "$DEV" ] || [ "$DEV" = "null" ]; then
      echo "WARN: could not discover a device id; parameterised routes skipped" >&2
      DEV=""
    fi
    echo "discovered ids: DEV=$DEV"
  fi
  mkdir -p "$outdir"; echo "DEV='$DEV'" > "$outdir/IDS"
}

capture() {
  local outdir="$1"
  rm -rf "$outdir"; mkdir -p "$outdir/authed" "$outdir/unauth" "$outdir/slash" "$outdir/param"
  discover_ids "$outdir"

  echo "# API=$API" > "$outdir/MANIFEST"
  echo "# captured=$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$outdir/MANIFEST"

  echo "== authenticated (via pvr curl) =="
  for p in "${AUTHED_PATHS[@]}"; do
    local s; s="$(slug "$p")"
    timeout 60 pvr curl -s -D "$outdir/authed/$s.hdr.raw" -o "$outdir/authed/$s.body.raw" \
      -A "$UA" "$API$p" >/dev/null 2>&1
    finalise "$outdir/authed/$s" "$p"
  done

  echo "== unauthenticated (401 contract) =="
  for p in "${UNAUTH_PATHS[@]}"; do
    local s; s="$(slug "$p")"
    curl -s -D "$outdir/unauth/$s.hdr.raw" -o "$outdir/unauth/$s.body.raw" \
      -A "$UA" --max-time 20 "$API$p" >/dev/null 2>&1
    finalise "$outdir/unauth/$s" "$p"
  done

  echo "== path-parameter routes (#id -> :id must be a no-op) =="
  if [ -n "${DEV:-}" ]; then
    for tpl in "${PARAM_PATHS[@]}"; do
      local p s
      p="${tpl//\{DEV\}/$DEV}"
      # Slug from the TEMPLATE, not the concrete path, so filenames stay stable
      # across environments and the diff lines up even if the id changes.
      s="$(slug "${tpl//\{DEV\}/ID}")"
      timeout 60 pvr curl -s -D "$outdir/param/$s.hdr.raw" -o "$outdir/param/$s.body.raw" \
        -A "$UA" "$API$p" >/dev/null 2>&1
      # Mask the concrete id so baseline and candidate agree textually.
      sed -i "s/$DEV/<DEV>/g" "$outdir/param/$s.body.raw" 2>/dev/null
      finalise "$outdir/param/$s" "$tpl"
    done
  else
    echo "(skipped: no device id)"
  fi

  echo "== malformed identifiers (current behaviour, incl. the /trails 500) =="
  for p in "${BADPARAM_PATHS[@]}"; do
    local s; s="$(slug "$p")"
    timeout 60 pvr curl -s -D "$outdir/param/$s.hdr.raw" -o "$outdir/param/$s.body.raw" \
      -A "$UA" "$API$p" >/dev/null 2>&1
    finalise "$outdir/param/$s" "$p"
  done

  echo "== trailing-slash pairs =="
  : > "$outdir/slash/pairs.txt"
  for p in "${SLASH_PATHS[@]}"; do
    local a b
    a="$(curl -s -o /dev/null -w '%{http_code}' -A "$UA" --max-time 20 "$API$p")"
    b="$(curl -s -o /dev/null -w '%{http_code}' -A "$UA" --max-time 20 "$API$p/")"
    printf '%-20s noslash=%s slash=%s\n' "$p" "$a" "$b" >> "$outdir/slash/pairs.txt"
  done
  cat "$outdir/slash/pairs.txt"

  find "$outdir" -name '*.raw' -delete
  echo
  echo "captured -> $outdir"
}

# Reduce a raw header dump + body into the two stable files we actually diff.
finalise() {
  local base="$1" path="$2" status
  {
    head -1 "$base.hdr.raw" 2>/dev/null | tr -d '\r'
    grep -iE "$CONTRACT_HEADERS" "$base.hdr.raw" 2>/dev/null | tr -d '\r' \
      | sed 's/^\([A-Za-z-]*\):/\L\1:/' | sort
  } > "$base.hdr"
  status="$(head -1 "$base.hdr")"

  if [[ "$path" =~ $BODY_SKIP ]]; then
    echo "<BODY NOT DIFFED: live data; see BODY_SKIP in capture.sh>" > "$base.body"
  else
    normalise_body "$base.body.raw" > "$base.body" 2>/dev/null
  fi

  # Error shape is contract even where the body itself is too live to diff.
  case "$status" in
    *" 4"*|*" 5"*) echo "$(error_shape "$base.body.raw")" > "$base.errshape" ;;
  esac

  printf '%-18s %s\n' "$path" "$status"
}

case "$MODE" in
  baseline)  capture "$HERE/baseline" ;;
  candidate) capture "$HERE/candidate" ;;
  diff)
    if [ ! -d "$HERE/baseline" ] || [ ! -d "$HERE/candidate" ]; then
      echo "need both $HERE/baseline and $HERE/candidate" >&2; exit 2
    fi
    if diff -ru "$HERE/baseline" "$HERE/candidate" -x MANIFEST; then
      echo "IDENTICAL - no contract regression"; exit 0
    else
      echo; echo "DIFFERENCES FOUND - each one is a regression unless intended" >&2; exit 1
    fi ;;
  *) sed -n '2,24p' "${BASH_SOURCE[0]}" | sed 's/^# \?//'; exit 2 ;;
esac
