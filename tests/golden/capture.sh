#!/usr/bin/env bash
#
# Golden-response capture for the go-json-rest -> echo migration: records the
# HTTP contract (status, contract headers, normalised bodies) of a running API.
# Capture a baseline on the old build, a candidate on the new one, then diff.
# Auth uses `pvr curl`, so no credentials are stored.
#
# Usage: API=https://api.stage.pantahub.com tests/golden/capture.sh baseline|candidate|diff
# diff exits 0 when identical, 1 on differences.

set -uo pipefail

API="${API:-https://api.stage.pantahub.com}"
UA="${UA:-pantahub-golden/0.1}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODE="${1:-}"

# Contract headers; volatile ones are ignored. x-powered-by dropped on echo
# (owner decision), so not pinned.
CONTRACT_HEADERS='^(www-authenticate|phjsonformat|content-type|location|allow):'

# Authenticated GETs (via pvr curl).
AUTHED_PATHS=(
  /devices/ /trails/ /apps/ /objects/ /logs/ /dash/
  /profiles/ /subscriptions/ /tokens/ /exports/ /metrics/
  # dash (first service moved to echo)
  /dash/auth_status /dash/does-not-exist
  # plog
  /plog/posts /plog/does-not-exist
  # changes (no page[size] answers an empty 200: pre-existing bug, pinned)
  "/changes/devices?page%5Bsize%5D=5" /changes/devices /changes/does-not-exist
  # tokens, profiles
  /tokens/does-not-exist /profiles/config/meta /profiles/miele-devices
  # metrics
  /metrics/does-not-exist
  # subscriptions (URLClean: trailing "/" dropped before routing)
  /subscriptions/does-not-exist/
  # exports
  /exports/does-not-exist /exports/nobody-probe/nodevice/0/probe.tar
  # logs
  "/logs/?page=5" /logs/cursor /logs/does-not-exist
  # apps
  /apps/scopes /apps/does-not-exist
  # objects (pvr clone/push)
  /objects/auth_status /objects/does-not-exist /objects/does-not-exist/blob /objects/a/b/c
  # trails (canonical JSON, URLClean)
  /trails/summary /trails/does-not-exist/steps/ /trails/a/b/c/d/e/f/g/h
  # devices (overlapping routes; 404 vs 405)
  /devices/tokens /devices/auth_status /devices/np/nobody-probe/nodevice /devices/tokens/ownership/validate
  /devices/tokens/t1/public /devices/a/b/c/d
)

# No credentials: pins the 401 WWW-Authenticate contract pvr depends on.
UNAUTH_PATHS=(
  /auth/login /devices/ /trails/ /objects/ /apps/ /logs/ /profiles/
  /subscriptions/ /tokens/ /metrics/ /healthz/ /exports/
  # Auth runs before routing: unknown route without credentials is 401.
  /dash/ /dash/does-not-exist /devices/does-not-exist
  /plog/posts /plog/does-not-exist
  /changes/devices /tokens/does-not-exist /profiles/config/meta
  /metrics/does-not-exist /healthz/does-not-exist
  /subscriptions/does-not-exist
  /exports/does-not-exist /exports/nobody-probe/nodevice/0/probe.tar
  /logs/cursor /apps/does-not-exist
  # /apps/scopes is the one apps route without auth
  /apps/scopes
  /objects/auth_status /objects/does-not-exist/blob
  /webhooks/ /webhooks/event-types /webhooks/a/b/c/d
  /trails/summary /trails/does-not-exist/steps/
  /devices/tokens /devices/np/nobody-probe/nodevice /devices/register
)

# "METHOD|path|Authorization". callbacks/cron (saadmin Basic auth) probed without
# the secret: missing/wrong -> 401 + challenge, malformed -> 400. No handler runs.
RAW_PROBES=(
  "PUT|/callbacks/devices/probe00000000000000000000|"
  "PUT|/callbacks/devices/probe00000000000000000000|__WRONG_BASIC__"
  "PUT|/callbacks/devices/probe00000000000000000000|Basic !!!not-base64"
  "GET|/callbacks/devices/probe00000000000000000000|"
  "PUT|/callbacks/does-not-exist|"
  "PUT|/cron/public/devices|"
  "PUT|/cron/public/devices|__WRONG_BASIC__"
  "PUT|/cron/public/devices|Basic !!!not-base64"
  "GET|/cron/public/steps|"
  "GET|/healthz/|__WRONG_BASIC__"
  "GET|/healthz/|Basic !!!not-base64"
  "POST|/healthz/|"
  "PUT|/subscriptions/admin/subscription/|"
  "GET|/subscriptions/admin/subscription|"
)
WRONG_BASIC='Basic c2FhZG1pbjpkZWZpbml0ZWx5LW5vdC10aGUtc2VjcmV0'

# CORS preflights "path|method|headers"; config differs per service.
PREFLIGHTS=(
  "/dash/|GET|authorization,content-type"
  "/dash/|GET|x-not-allowed-header"
  "/dash/|DELETE|authorization"
  "/devices/|GET|authorization,content-type"
  "/trails/|GET|authorization,content-type"
  "/trails/|PATCH|authorization"
  "/objects/|GET|authorization"
  "/apps/|GET|authorization"
)

# No-slash form is a 307 from http.ServeMux.
SLASH_PATHS=(
  /devices /trails /apps /objects /logs /profiles /subscriptions /tokens
)

# Path-parameter routes; {DEV} is discovered at capture time.
PARAM_PATHS=(
  "/devices/{DEV}"
  "/devices/{DEV}/user-meta"
  "/trails/{DEV}"
  "/trails/{DEV}/steps"
  "/trails/{DEV}/summary"
  "/trails/{DEV}/steps/0"
  # Missing revision: 404 since the ErrNoDocuments fix (was 500).
  "/trails/{DEV}/steps/999999"
)

# Malformed ids pin current behaviour (/devices 400, /trails 500).
BAD_ID='nonexistent0000000000000000'
BADPARAM_PATHS=(
  "/devices/$BAD_ID"
  "/trails/$BAD_ID"
  "/trails/$BAD_ID/steps"
)

slug() { echo "$1" | sed 's#^/##; s#/$##; s#/#_#g; s#[^A-Za-z0-9_.-]#-#g; s#^$#root#'; }

# Live-data endpoints: status and headers only.
BODY_SKIP='^/(logs|devices|dash)/(\?.*)?$'

# Mask volatile values so diffs show contract changes only.
normalise_body() {
  local f="$1"
  if command -v jq >/dev/null 2>&1 && jq -e . >/dev/null 2>&1 < "$f"; then
    # Device telemetry and live fleet state change between captures.
    jq -S 'walk(if type == "object" then
                  with_entries(.value =
                    if (.key | test("^(device-meta|sysinfo|storage)$"; "i"))
                    then "<VOLATILE-SUBTREE>"
                    elif (.key | test("^(fleet\\.|progress-revision$|progress-time$|revision$|state-sha$|step-time$|trail-touched-time$)"; "i"))
                    then "<VAR>"
                    elif (.key | test("^(id|_id|timestamp|time-?modified|time-?created|last-?seen|rev|garbage|exp|iat|tsec|tnano|dev|device|trail-touched|status-changed|meta-modified|last-?touched|last-?insync|orig_iat|jti)$"; "i"))
                    then "<VAR>" else .value end)
                else . end)' < "$f" 2>/dev/null \
      | sed -E 's/REST-ERR-ID-[0-9]+/REST-ERR-ID-<VAR>/g; s/page\[(before|after)\]=[^"&]*/page[\1]=<VAR>/g'
  else
    sed -E 's/REST-ERR-ID-[0-9]+/REST-ERR-ID-<VAR>/g; s/page\[(before|after)\]=[^"&]*/page[\1]=<VAR>/g' < "$f"
  fi
}

# Record which error shape a route uses: {"Error"} or {"code","error"}.
error_shape() {
  local f="$1"
  if   grep -q '"Error"'   "$f" 2>/dev/null; then echo 'gjr:{"Error":...}'
  elif grep -q 'REST-ERR-ID' "$f" 2>/dev/null; then echo 'rerror:{"code","error"}+incident-id'
  elif [ -s "$f" ]; then echo 'other'
  else echo 'empty'; fi
}

# Baseline discovers ids; candidate reuses them so both hit the same resource.
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

# Fetch into <base>.hdr.raw/.body.raw, retrying once on 5xx (stage Elasticsearch
# timeouts). Retries are logged; noretry for probes whose contract is a 5xx.
fetch() {
  local base="$1" url="$2" authed="$3" noretry="${4:-}" attempt status
  for attempt in 1 2; do
    if [ "$authed" = "auth" ]; then
      timeout 60 pvr curl -s -D "$base.hdr.raw" -o "$base.body.raw" -A "$UA" "$url" >/dev/null 2>&1
    else
      curl -s -D "$base.hdr.raw" -o "$base.body.raw" -A "$UA" --max-time 20 "$url" >/dev/null 2>&1
    fi
    status="$(head -1 "$base.hdr.raw" 2>/dev/null | tr -d '\r')"
    case "$status" in
      *" 5"*)
        [ -n "$noretry" ] && break
        [ "$attempt" = 1 ] && { echo "  retrying after transient $status on $url" >&2; sleep 2; continue; } ;;
    esac
    break
  done
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
    fetch "$outdir/authed/$s" "$API$p" auth
    finalise "$outdir/authed/$s" "$p"
  done

  echo "== unauthenticated (401 contract) =="
  for p in "${UNAUTH_PATHS[@]}"; do
    local s; s="$(slug "$p")"
    fetch "$outdir/unauth/$s" "$API$p" noauth
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
      fetch "$outdir/param/$s" "$API$p" auth
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
    fetch "$outdir/param/$s" "$API$p" auth noretry
    finalise "$outdir/param/$s" "$p"
  done

  echo "== raw method/Authorization probes =="
  mkdir -p "$outdir/raw"
  for spec in "${RAW_PROBES[@]}"; do
    local m p authz s
    IFS='|' read -r m p authz <<< "$spec"
    [ "$authz" = "__WRONG_BASIC__" ] && authz="$WRONG_BASIC"
    case "$authz" in
      "")      s="$(slug "$p")_${m}_noauth" ;;
      "$WRONG_BASIC") s="$(slug "$p")_${m}_wrongbasic" ;;
      *)       s="$(slug "$p")_${m}_malformed" ;;
    esac
    if [ -n "$authz" ]; then
      curl -s -X "$m" -D "$outdir/raw/$s.hdr.raw" -o "$outdir/raw/$s.body.raw" \
        -A "$UA" --max-time 20 -H "Authorization: $authz" "$API$p" >/dev/null 2>&1
    else
      curl -s -X "$m" -D "$outdir/raw/$s.hdr.raw" -o "$outdir/raw/$s.body.raw" \
        -A "$UA" --max-time 20 "$API$p" >/dev/null 2>&1
    fi
    finalise "$outdir/raw/$s" "$m $p ${s##*_}"
  done

  echo "== CORS preflights =="
  mkdir -p "$outdir/cors"
  for spec in "${PREFLIGHTS[@]}"; do
    local p m hdrs s
    IFS='|' read -r p m hdrs <<< "$spec"
    s="$(slug "$p")_${m}_$(echo "$hdrs" | tr -c 'A-Za-z0-9' '_')"
    curl -s -o /dev/null -D "$outdir/cors/$s.raw" -X OPTIONS -A "$UA" --max-time 20 \
      -H "Origin: https://hub.stage.pantahub.com" \
      -H "Access-Control-Request-Method: $m" \
      -H "Access-Control-Request-Headers: $hdrs" "$API$p" >/dev/null 2>&1
    {
      head -1 "$outdir/cors/$s.raw" | tr -d '\r'
      grep -iE '^(access-control-|content-type:)' "$outdir/cors/$s.raw" | tr -d '\r' \
        | sed 's/^\([A-Za-z-]*\):/\L\1:/' | sort
    } > "$outdir/cors/$s.hdr"
    rm -f "$outdir/cors/$s.raw"
    printf '%-40s %s\n' "$p $m [$hdrs]" "$(head -1 "$outdir/cors/$s.hdr")"
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
