#!/usr/bin/env bash
# scripts/dev/smoke-backend.sh
#
# End-to-end smoke test for the SnoreGuard backend stack. Hits every
# endpoint at least once against a local docker-compose-up-d cluster.
#
# Audience: a developer who has just run
#     docker compose --profile app up -d
# (and optionally `--profile otel`) and wants to confirm the stack
# works end-to-end before pointing the iOS app at it.
#
# This script NEVER runs against production. The pre-flight check
# refuses any non-localhost target.
#
# Privacy:
#   - bearer tokens are read from env vars and passed to curl via
#     -H so they never appear on the command line.
#   - Apple identity tokens are masked under --debug.
#   - synthetic clip bytes live in memory only; never written to disk.
#
# Inputs (env):
#   BACKEND_BASE_SYNC       (default http://localhost:8080)
#   BACKEND_BASE_ANALYTICS  (default http://localhost:8081)
#   BACKEND_BASE_EXPORT     (default http://localhost:8082)
#   OTEL_PROMETHEUS_URL     (default http://localhost:9464/metrics)
#   SNOREGUARD_TEST_APPLE_TOKEN  (real Apple identity token; required
#                                 unless --mock-auth is passed)
#
# Flags:
#   --debug         echo every curl with -v (tokens still redacted)
#   --mock-auth     skip every step that needs a real Apple Sign-In
#                   identity token; exits 0 after the unauthed steps.

set -euo pipefail

# ---- defaults ---------------------------------------------------------------

: "${BACKEND_BASE_SYNC:=http://localhost:8080}"
: "${BACKEND_BASE_ANALYTICS:=http://localhost:8081}"
: "${BACKEND_BASE_EXPORT:=http://localhost:8082}"
: "${OTEL_PROMETHEUS_URL:=http://localhost:9464/metrics}"

DEBUG=0
MOCK_AUTH=0

while [ $# -gt 0 ]; do
    case "$1" in
        --debug)     DEBUG=1; shift ;;
        --mock-auth) MOCK_AUTH=1; shift ;;
        -h|--help)
            sed -n '2,32p' "$0"
            exit 0
            ;;
        *)
            printf 'unknown flag: %s\n' "$1" >&2
            exit 2
            ;;
    esac
done

TOTAL_STEPS=18
STEP=0

# ---- output helpers ---------------------------------------------------------

step() {
    STEP=$((STEP + 1))
    printf '[%2d/%d] %s ... ' "$STEP" "$TOTAL_STEPS" "$1"
}

ok()       { printf 'OK%s\n' "${1:+ ($1)}"; }
skipped()  { printf 'SKIP (%s)\n' "$1"; }
failed()   {
    printf 'FAIL\n'
    printf '       reason: %s\n' "$1" >&2
    if [ -n "${2:-}" ]; then
        printf '       reproduce: %s\n' "$2" >&2
    fi
    exit 1
}

note() { printf '       %s\n' "$1"; }

mask() {
    # mask "$1" -> first 6 chars + ellipsis + last 4 chars (or "<empty>")
    local s="${1:-}"
    if [ -z "$s" ]; then
        printf '<empty>'
    elif [ ${#s} -le 12 ]; then
        printf '<%d-char-token-redacted>' "${#s}"
    else
        printf '%s...%s' "${s:0:6}" "${s: -4}"
    fi
}

# ---- pre-flight: localhost only ---------------------------------------------

require_localhost() {
    local url="$1"
    local label="$2"
    case "$url" in
        http://localhost*|http://127.0.0.1*|http://0.0.0.0*) ;;
        'http://[::1]'*) ;;
        http://host.docker.internal*) ;;
        *)
            printf 'refusing to run %s against non-localhost URL: %s\n' "$label" "$url" >&2
            printf 'this script targets local docker-compose only.\n' >&2
            exit 2
            ;;
    esac
}

require_localhost "$BACKEND_BASE_SYNC" BACKEND_BASE_SYNC
require_localhost "$BACKEND_BASE_ANALYTICS" BACKEND_BASE_ANALYTICS
require_localhost "$BACKEND_BASE_EXPORT" BACKEND_BASE_EXPORT
require_localhost "$OTEL_PROMETHEUS_URL" OTEL_PROMETHEUS_URL

# ---- dependency check -------------------------------------------------------

for bin in curl jq python3; do
    if ! command -v "$bin" >/dev/null 2>&1; then
        printf 'missing dependency: %s\n' "$bin" >&2
        exit 2
    fi
done

# ---- curl wrappers ----------------------------------------------------------
# Tokens are passed via env to avoid leaking them onto the process
# table. With --debug we still mask them in the printed header.

CURL_OPTS=(--silent --show-error --max-time 30 --fail-with-body)
if [ "$DEBUG" = "1" ]; then
    CURL_OPTS+=(--verbose)
fi

# curl_unauth <method> <url>          -> writes body to stdout
curl_unauth() {
    local method="$1"
    local url="$2"
    if [ "$DEBUG" = "1" ]; then
        printf 'DEBUG: curl -X %s %s\n' "$method" "$url" >&2
    fi
    curl "${CURL_OPTS[@]}" -X "$method" "$url"
}

# curl_unauth_json <method> <url> <json-body>
curl_unauth_json() {
    local method="$1"
    local url="$2"
    local body="$3"
    if [ "$DEBUG" = "1" ]; then
        printf 'DEBUG: curl -X %s %s (json body, %d bytes)\n' \
            "$method" "$url" "${#body}" >&2
    fi
    curl "${CURL_OPTS[@]}" -X "$method" \
         -H 'Content-Type: application/json' \
         --data-raw "$body" \
         "$url"
}

# curl_auth_json <method> <url> <json-body-or-empty>  (uses $ACCESS_TOKEN)
curl_auth_json() {
    local method="$1"
    local url="$2"
    local body="${3:-}"
    if [ -z "${ACCESS_TOKEN:-}" ]; then
        failed "internal: curl_auth_json called without ACCESS_TOKEN"
    fi
    if [ "$DEBUG" = "1" ]; then
        printf 'DEBUG: curl -X %s %s -H "Authorization: Bearer %s"\n' \
            "$method" "$url" "$(mask "$ACCESS_TOKEN")" >&2
    fi
    if [ -n "$body" ]; then
        curl "${CURL_OPTS[@]}" -X "$method" \
             -H "Authorization: Bearer ${ACCESS_TOKEN}" \
             -H 'Content-Type: application/json' \
             --data-raw "$body" \
             "$url"
    else
        curl "${CURL_OPTS[@]}" -X "$method" \
             -H "Authorization: Bearer ${ACCESS_TOKEN}" \
             "$url"
    fi
}

# curl_auth_get_with_status <url> -> writes "<status>\n<body>"
curl_auth_get_with_status() {
    local url="$1"
    if [ "$DEBUG" = "1" ]; then
        printf 'DEBUG: curl GET %s -H "Authorization: Bearer %s"\n' \
            "$url" "$(mask "$ACCESS_TOKEN")" >&2
    fi
    # Don't use --fail-with-body here; we want to inspect status.
    local raw
    raw="$(curl --silent --show-error --max-time 30 \
                -H "Authorization: Bearer ${ACCESS_TOKEN}" \
                -w '\n__HTTP_STATUS__:%{http_code}\n__CONTENT_TYPE__:%{content_type}\n' \
                "$url")"
    printf '%s' "$raw"
}

# uuidgen via python (portable: Linux + macOS without uuidgen).
gen_uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }

# rfc3339 timestamp now (UTC, seconds, "Z").
rfc3339_now() { python3 -c 'from datetime import datetime,timezone; print(datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))'; }
rfc3339_at()  { python3 -c "from datetime import datetime,timezone,timedelta; print((datetime.now(timezone.utc)+timedelta(seconds=$1)).strftime('%Y-%m-%dT%H:%M:%SZ'))"; }

# ---- step 1: healthz on each service ----------------------------------------

step "healthz on sync-service"
HEALTH_SYNC="$(curl_unauth GET "$BACKEND_BASE_SYNC/healthz")" || \
    failed "sync-service /healthz failed" "curl $BACKEND_BASE_SYNC/healthz"
echo "$HEALTH_SYNC" | jq -e '.status == "ok"' >/dev/null || \
    failed "sync-service /healthz did not return status=ok" "curl $BACKEND_BASE_SYNC/healthz"
ok "$BACKEND_BASE_SYNC"

step "healthz on analytics-service"
HEALTH_AN="$(curl_unauth GET "$BACKEND_BASE_ANALYTICS/healthz")" || \
    failed "analytics-service /healthz failed" "curl $BACKEND_BASE_ANALYTICS/healthz"
echo "$HEALTH_AN" | jq -e '.status == "ok"' >/dev/null || \
    failed "analytics-service /healthz did not return status=ok" "curl $BACKEND_BASE_ANALYTICS/healthz"
ok "$BACKEND_BASE_ANALYTICS"

step "healthz on export-service"
HEALTH_EX="$(curl_unauth GET "$BACKEND_BASE_EXPORT/healthz")" || \
    failed "export-service /healthz failed" "curl $BACKEND_BASE_EXPORT/healthz"
echo "$HEALTH_EX" | jq -e '.status == "ok"' >/dev/null || \
    failed "export-service /healthz did not return status=ok" "curl $BACKEND_BASE_EXPORT/healthz"
ok "$BACKEND_BASE_EXPORT"

# ---- auth gate --------------------------------------------------------------

if [ "$MOCK_AUTH" = "1" ]; then
    note "--mock-auth is set; skipping all Apple-Sign-In-dependent steps."
    note "the smoke check stops here. Pass SNOREGUARD_TEST_APPLE_TOKEN for the full run."
    # Burn the remaining step counters so the user sees the gap explicitly.
    for label in \
        "POST /auth/apple" \
        "POST /sessions" \
        "GET /sessions" \
        "POST /events" \
        "GET /events" \
        "POST /audio/clips (multipart)" \
        "GET /audio/clips" \
        "GET /audio/clips/{id}/download" \
        "GET /export/events.csv" \
        "GET /export/events.json" \
        "GET /analytics/totals" \
        "GET /analytics/summary" \
        "POST /auth/refresh" \
        "POST /auth/logout" \
        "POST /events with revoked token (expect 401)" \
        "DELETE /audio/clips/{id} (post-logout, may skip)"
    do
        step "$label"
        skipped "requires real Apple Sign-In auth"
    done
    # Optional Prometheus scrape can still run (no auth needed).
    : "${SKIP_OTEL:=0}"
else
    if [ -z "${SNOREGUARD_TEST_APPLE_TOKEN:-}" ]; then
        cat >&2 <<'EOF'

ERROR: SNOREGUARD_TEST_APPLE_TOKEN is not set.

The /auth/apple endpoint validates a real Apple identity token signed by
appleid.apple.com. We cannot synthesize a usable one in this script: Apple's
signature is the whole point. To exercise the full stack, you need either:

  (a) a real Apple identity token captured from a sign-in attempt against
      the same APPLE_AUDIENCE the backend is configured for, OR
  (b) a backend dev-mode bypass that accepts a fake token (planned, not
      yet landed).

If you only want to confirm the stack is up (healthz on all three
services), re-run with --mock-auth.

EOF
        exit 1
    fi

    # ---- step 2: POST /auth/apple ------------------------------------------

    step "POST /auth/apple"
    AUTH_BODY="$(jq -n --arg t "$SNOREGUARD_TEST_APPLE_TOKEN" '{identity_token:$t}')"
    AUTH_RESP="$(curl_unauth_json POST "$BACKEND_BASE_SYNC/auth/apple" "$AUTH_BODY")" || \
        failed "/auth/apple rejected the identity token" \
               "curl -X POST $BACKEND_BASE_SYNC/auth/apple -d '{\"identity_token\":\"<token>\"}'"
    ACCESS_TOKEN="$(echo "$AUTH_RESP" | jq -r '.access_token // empty')"
    REFRESH_TOKEN="$(echo "$AUTH_RESP" | jq -r '.refresh_token // empty')"
    USER_ID="$(echo "$AUTH_RESP" | jq -r '.user_id // empty')"
    ACCESS_EXP="$(echo "$AUTH_RESP" | jq -r '.access_token_expires_at // empty')"
    if [ -z "$ACCESS_TOKEN" ] || [ -z "$REFRESH_TOKEN" ] || [ -z "$USER_ID" ]; then
        failed "/auth/apple response missing access_token / refresh_token / user_id"
    fi
    ok "user=$USER_ID expires=$ACCESS_EXP"

    # ---- step 3: POST /sessions --------------------------------------------

    step "POST /sessions"
    CLIENT_SESSION_ID="$(gen_uuid)"
    SESSION_STARTED_AT="$(rfc3339_now)"
    SESS_BODY="$(jq -n \
        --arg cid "$CLIENT_SESSION_ID" \
        --arg s   "$SESSION_STARTED_AT" \
        '{client_session_id:$cid, started_at:$s, device_name:"smoke-script", app_version:"0.0.0-smoke"}')"
    SESS_RESP="$(curl_auth_json POST "$BACKEND_BASE_SYNC/sessions" "$SESS_BODY")" || \
        failed "POST /sessions failed" "curl -X POST $BACKEND_BASE_SYNC/sessions"
    SESSION_ID="$(echo "$SESS_RESP" | jq -r '.session_id // empty')"
    SESSION_CREATED="$(echo "$SESS_RESP" | jq -r '.created // empty')"
    if [ -z "$SESSION_ID" ]; then
        failed "POST /sessions returned no session_id (got: $SESS_RESP)"
    fi
    ok "session_id=$SESSION_ID created=$SESSION_CREATED"

    # ---- step 4: GET /sessions ---------------------------------------------

    step "GET /sessions"
    SESS_LIST="$(curl_auth_json GET "$BACKEND_BASE_SYNC/sessions?limit=10")" || \
        failed "GET /sessions failed"
    if ! echo "$SESS_LIST" | jq -e --arg sid "$SESSION_ID" \
            'any(.sessions[]?; .session_id == $sid)' >/dev/null; then
        failed "GET /sessions did not include the session we just created ($SESSION_ID)"
    fi
    SESS_COUNT="$(echo "$SESS_LIST" | jq -r '.sessions | length')"
    ok "found new session in list (total returned: $SESS_COUNT)"

    # ---- step 5: POST /events ----------------------------------------------

    step "POST /events"
    EV1="$(gen_uuid)"; EV2="$(gen_uuid)"; EV3="$(gen_uuid)"
    EV1_AT="$(rfc3339_at -180)"; EV2_AT="$(rfc3339_at -120)"; EV3_AT="$(rfc3339_at -60)"
    EVENTS_BODY="$(jq -n \
        --arg s   "$SESSION_ID" \
        --arg e1  "$EV1" --arg e1at "$EV1_AT" \
        --arg e2  "$EV2" --arg e2at "$EV2_AT" \
        --arg e3  "$EV3" --arg e3at "$EV3_AT" \
        '{events:[
            {client_event_id:$e1, started_at:$e1at, duration_ms:1500, avg_db:62.5, session_id:$s},
            {client_event_id:$e2, started_at:$e2at, duration_ms:1700, avg_db:64.0, session_id:$s},
            {client_event_id:$e3, started_at:$e3at, duration_ms:1300, avg_db:60.0, session_id:$s}
         ]}')"
    EVENTS_RESP="$(curl_auth_json POST "$BACKEND_BASE_SYNC/events" "$EVENTS_BODY")" || \
        failed "POST /events failed"
    INSERTED="$(echo "$EVENTS_RESP" | jq -r '.inserted // empty')"
    RECEIVED="$(echo "$EVENTS_RESP" | jq -r '.received // empty')"
    if [ "$INSERTED" != "3" ] || [ "$RECEIVED" != "3" ]; then
        failed "POST /events expected inserted=3 received=3, got inserted=$INSERTED received=$RECEIVED"
    fi
    ok "inserted=3 received=3"

    # ---- step 6: GET /events -----------------------------------------------

    step "GET /events"
    EV_LIST="$(curl_auth_json GET "$BACKEND_BASE_SYNC/events?limit=100")" || \
        failed "GET /events failed"
    for eid in "$EV1" "$EV2" "$EV3"; do
        if ! echo "$EV_LIST" | jq -e --arg id "$eid" \
                'any(.events[]?; .client_event_id == $id)' >/dev/null; then
            failed "GET /events did not return client_event_id=$eid"
        fi
    done
    ok "all 3 events present"

    # ---- step 7: POST /audio/clips (multipart) -----------------------------

    step "POST /audio/clips (multipart, 4 KiB)"
    CLIP_CLIENT_ID="$(gen_uuid)"
    CLIP_STARTED_AT="$(rfc3339_at -90)"

    # Generate, hash, multipart-encode, and curl-upload — entirely in
    # python so the binary body never touches the shell. Keeps the
    # 4 KiB random buffer in memory only.
    #
    # We pre-build the metadata JSON in bash (jq) and pass it through
    # env to python; python builds the multipart envelope and shells
    # out to curl with --data-binary @- so the body is streamed via
    # stdin (token never on the command line either).
    CLIP_META_JSON="$(jq -n \
        --arg cid "$CLIP_CLIENT_ID" \
        --arg sid "$SESSION_ID" \
        --arg sa  "$CLIP_STARTED_AT" \
        '{client_clip_id:$cid, session_id:$sid, started_at:$sa, duration_ms:1500, avg_db:62.5}')"

    CLIP_RESP_FILE="$(mktemp)"
    CLIP_HASH_FILE="$(mktemp)"
    trap 'rm -f "$CLIP_RESP_FILE" "$CLIP_HASH_FILE"' EXIT

    # Python heredoc builds the multipart envelope (boundaries +
    # metadata part with sha256 + 4 KiB random file part) and POSTs
    # via curl. Bash's printf-based multipart construction is fragile
    # for binary; python keeps bytes intact.
    BACKEND_BASE_SYNC="$BACKEND_BASE_SYNC" \
    ACCESS_TOKEN="$ACCESS_TOKEN" \
    CLIP_META_JSON="$CLIP_META_JSON" \
    CLIP_RESP_FILE="$CLIP_RESP_FILE" \
    CLIP_HASH_FILE="$CLIP_HASH_FILE" \
    DEBUG="$DEBUG" \
    python3 - <<'PYEOF' || failed "POST /audio/clips multipart upload failed"
import hashlib
import json
import os
import secrets
import subprocess
import sys
import uuid

base   = os.environ["BACKEND_BASE_SYNC"]
token  = os.environ["ACCESS_TOKEN"]
meta   = json.loads(os.environ["CLIP_META_JSON"])
debug  = os.environ.get("DEBUG", "0") == "1"
respfp = os.environ["CLIP_RESP_FILE"]
hashfp = os.environ["CLIP_HASH_FILE"]

# 4 KiB of random bytes; never written to disk.
body = secrets.token_bytes(4096)
sha  = hashlib.sha256(body).hexdigest()
meta["sha256"] = sha

with open(hashfp, "w") as f:
    f.write(sha)

boundary = "----snoreguard-smoke-" + uuid.uuid4().hex
crlf = b"\r\n"
parts = []
# metadata part
parts.append(("--" + boundary).encode())
parts.append(b'Content-Disposition: form-data; name="metadata"')
parts.append(b'Content-Type: application/json')
parts.append(b"")
parts.append(json.dumps(meta).encode())
# file part
parts.append(("--" + boundary).encode())
parts.append(b'Content-Disposition: form-data; name="file"; filename="clip.m4a"')
parts.append(b'Content-Type: audio/m4a')
parts.append(b"")
parts.append(body)
parts.append(("--" + boundary + "--").encode())
parts.append(b"")
envelope = crlf.join(parts)

cmd = [
    "curl", "--silent", "--show-error", "--max-time", "30", "--fail-with-body",
    "-X", "POST",
    "-H", f"Authorization: Bearer {token}",
    "-H", f"Content-Type: multipart/form-data; boundary={boundary}",
    "--data-binary", "@-",
    f"{base}/audio/clips",
]
if debug:
    # Don't echo the binary or the token. Print metadata + size only.
    masked = (token[:6] + "..." + token[-4:]) if len(token) > 12 else "<masked>"
    sys.stderr.write(
        f"DEBUG: curl POST {base}/audio/clips "
        f"(multipart, {len(envelope)} bytes total, "
        f"file={len(body)} bytes, sha256={sha[:8]}..., "
        f"Auth=Bearer {masked})\n"
    )
proc = subprocess.run(cmd, input=envelope, capture_output=True)
with open(respfp, "wb") as f:
    f.write(proc.stdout)
if proc.returncode != 0:
    sys.stderr.write(proc.stderr.decode(errors="replace"))
    sys.exit(proc.returncode)
PYEOF

    CLIP_RESP="$(cat "$CLIP_RESP_FILE")"
    CLIP_LOCAL_SHA="$(cat "$CLIP_HASH_FILE")"
    CLIP_ID="$(echo "$CLIP_RESP" | jq -r '.clip_id // empty')"
    OBJECT_KEY="$(echo "$CLIP_RESP" | jq -r '.object_key // empty')"
    UPLOADED_AT="$(echo "$CLIP_RESP" | jq -r '.uploaded_at // empty')"
    if [ -z "$CLIP_ID" ]; then
        failed "POST /audio/clips returned no clip_id (got: $CLIP_RESP)"
    fi
    ok "clip_id=$CLIP_ID key=$OBJECT_KEY uploaded_at=$UPLOADED_AT"

    # ---- step 8: GET /audio/clips ------------------------------------------

    step "GET /audio/clips"
    CLIPS_LIST="$(curl_auth_json GET "$BACKEND_BASE_SYNC/audio/clips?limit=10")" || \
        failed "GET /audio/clips failed"
    LIST_SHA="$(echo "$CLIPS_LIST" | jq -r --arg id "$CLIP_ID" \
        '.clips[]? | select(.clip_id == $id) | .sha256 // empty')"
    if [ -z "$LIST_SHA" ]; then
        failed "GET /audio/clips did not include clip_id=$CLIP_ID"
    fi
    if [ "$LIST_SHA" != "$CLIP_LOCAL_SHA" ]; then
        failed "GET /audio/clips sha256 mismatch (server=$LIST_SHA local=$CLIP_LOCAL_SHA)"
    fi
    ok "sha256 matches"

    # ---- step 9: GET /audio/clips/{id}/download ----------------------------

    step "GET /audio/clips/{id}/download"
    DL_SHA="$(curl --silent --show-error --max-time 30 --fail-with-body \
                -H "Authorization: Bearer ${ACCESS_TOKEN}" \
                "$BACKEND_BASE_SYNC/audio/clips/${CLIP_ID}/download" \
              | python3 -c 'import hashlib,sys; print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())')" || \
        failed "GET /audio/clips/{id}/download failed"
    if [ "$DL_SHA" != "$CLIP_LOCAL_SHA" ]; then
        failed "downloaded clip sha256 mismatch (download=$DL_SHA local=$CLIP_LOCAL_SHA)"
    fi
    ok "round-trip sha256 matches"

    # ---- step 10: GET /export/events.csv -----------------------------------

    step "GET /export/events.csv?include=sessions,clips"
    CSV_OUT="$(mktemp)"
    trap 'rm -f "$CLIP_RESP_FILE" "$CLIP_HASH_FILE" "$CSV_OUT"' EXIT
    HTTP_INFO="$(curl --silent --show-error --max-time 60 \
        -H "Authorization: Bearer ${ACCESS_TOKEN}" \
        -o "$CSV_OUT" \
        -w '%{http_code} %{content_type}' \
        "$BACKEND_BASE_EXPORT/export/events.csv?include=sessions,clips")"
    CSV_STATUS="${HTTP_INFO%% *}"
    CSV_CT="${HTTP_INFO#* }"
    if [ "$CSV_STATUS" != "200" ]; then
        failed "/export/events.csv returned $CSV_STATUS"
    fi
    case "$CSV_CT" in
        text/csv*) : ;;
        *) failed "/export/events.csv content-type was '$CSV_CT', expected text/csv*" ;;
    esac
    for marker in "# section: events" "# section: sessions" "# section: clips"; do
        if ! grep -qF "$marker" "$CSV_OUT"; then
            failed "CSV body missing marker '$marker'"
        fi
    done
    CSV_BYTES="$(wc -c < "$CSV_OUT")"
    ok "200 text/csv, ${CSV_BYTES} bytes, all 3 section markers present"

    # ---- step 11: GET /export/events.json ----------------------------------

    step "GET /export/events.json?include=sessions,clips"
    JSON_OUT="$(mktemp)"
    trap 'rm -f "$CLIP_RESP_FILE" "$CLIP_HASH_FILE" "$CSV_OUT" "$JSON_OUT"' EXIT
    JSON_INFO="$(curl --silent --show-error --max-time 60 \
        -H "Authorization: Bearer ${ACCESS_TOKEN}" \
        -o "$JSON_OUT" \
        -w '%{http_code}' \
        "$BACKEND_BASE_EXPORT/export/events.json?include=sessions,clips")"
    if [ "$JSON_INFO" != "200" ]; then
        failed "/export/events.json returned $JSON_INFO"
    fi
    if ! jq -e '.events | type == "array"' "$JSON_OUT" >/dev/null; then
        failed "/export/events.json missing .events[] (or not an array)"
    fi
    if ! jq -e '.sessions | type == "array"' "$JSON_OUT" >/dev/null; then
        failed "/export/events.json missing .sessions[] (or not an array)"
    fi
    if ! jq -e '.clips | type == "array"' "$JSON_OUT" >/dev/null; then
        failed "/export/events.json missing .clips[] (or not an array)"
    fi
    if ! jq -e '.count | type == "object"' "$JSON_OUT" >/dev/null; then
        failed "/export/events.json missing .count{} (with include=, count is a map)"
    fi
    JSON_BYTES="$(wc -c < "$JSON_OUT")"
    ok "events/sessions/clips arrays + count map, ${JSON_BYTES} bytes"

    # ---- step 12: GET /analytics/totals ------------------------------------

    step "GET /analytics/totals"
    TOTALS="$(curl_auth_json GET "$BACKEND_BASE_ANALYTICS/analytics/totals")" || \
        failed "GET /analytics/totals failed"
    if ! echo "$TOTALS" | jq -e '.event_count | type == "number"' >/dev/null; then
        failed "/analytics/totals missing event_count"
    fi
    EVT_COUNT="$(echo "$TOTALS" | jq -r '.event_count')"
    ok "event_count=$EVT_COUNT"

    # ---- step 13: GET /analytics/summary -----------------------------------

    step "GET /analytics/summary"
    SUMMARY_START="$(rfc3339_at -86400)"
    SUMMARY_END="$(rfc3339_at 3600)"
    SUMMARY_URL="$BACKEND_BASE_ANALYTICS/analytics/summary?start=${SUMMARY_START}&end=${SUMMARY_END}"
    SUMMARY="$(curl_auth_json GET "$SUMMARY_URL")" || \
        failed "GET /analytics/summary failed"
    if ! echo "$SUMMARY" | jq -e '.days | type == "array"' >/dev/null; then
        failed "/analytics/summary missing days[] (got: $SUMMARY)"
    fi
    DAYS="$(echo "$SUMMARY" | jq -r '.days | length')"
    ok "days[] length=$DAYS"

    # ---- step 14: POST /auth/refresh ---------------------------------------

    step "POST /auth/refresh"
    REFRESH_BODY="$(jq -n --arg t "$REFRESH_TOKEN" '{refresh_token:$t}')"
    REFRESH_RESP="$(curl_unauth_json POST "$BACKEND_BASE_SYNC/auth/refresh" "$REFRESH_BODY")" || \
        failed "POST /auth/refresh failed"
    NEW_ACCESS="$(echo "$REFRESH_RESP" | jq -r '.access_token // empty')"
    NEW_REFRESH="$(echo "$REFRESH_RESP" | jq -r '.refresh_token // empty')"
    if [ -z "$NEW_ACCESS" ] || [ -z "$NEW_REFRESH" ]; then
        failed "/auth/refresh did not return a new (access, refresh) pair"
    fi
    if [ "$NEW_ACCESS" = "$ACCESS_TOKEN" ]; then
        failed "/auth/refresh returned the same access token (rotation broken?)"
    fi
    if [ "$NEW_REFRESH" = "$REFRESH_TOKEN" ]; then
        failed "/auth/refresh returned the same refresh token (rotation broken?)"
    fi
    ACCESS_TOKEN="$NEW_ACCESS"
    REFRESH_TOKEN="$NEW_REFRESH"
    ok "rotated; new access=$(mask "$ACCESS_TOKEN")"

    # ---- step 15: POST /auth/logout ----------------------------------------

    step "POST /auth/logout"
    LOGOUT_BODY="$(jq -n --arg t "$REFRESH_TOKEN" '{refresh_token:$t}')"
    if ! curl_auth_json POST "$BACKEND_BASE_SYNC/auth/logout" "$LOGOUT_BODY" >/dev/null; then
        failed "POST /auth/logout failed"
    fi
    LOGOUT_HAPPENED=1
    ok "access JTI + refresh family revoked"

    # ---- step 16: revoked-access check ------------------------------------

    step "POST /events with revoked token (expect 401)"
    PROBE_AT="$(rfc3339_at -30)"
    PROBE_BODY="$(jq -n --arg s "$SESSION_ID" --arg eid "$(gen_uuid)" --arg sa "$PROBE_AT" \
        '{events:[{client_event_id:$eid, started_at:$sa, duration_ms:1000, avg_db:60.0, session_id:$s}]}')"
    PROBE_STATUS="$(curl --silent --show-error --max-time 30 \
        -H "Authorization: Bearer ${ACCESS_TOKEN}" \
        -H 'Content-Type: application/json' \
        --data-raw "$PROBE_BODY" \
        -o /dev/null \
        -w '%{http_code}' \
        "$BACKEND_BASE_SYNC/events")"
    if [ "$PROBE_STATUS" != "401" ]; then
        failed "expected 401 from POST /events with revoked token, got $PROBE_STATUS"
    fi
    ok "401 as expected (revocation works)"

    # ---- step 17: DELETE /audio/clips/{id} (post-logout, may skip) -----

    step "DELETE /audio/clips/{id} (post-logout)"
    if [ "${LOGOUT_HAPPENED:-0}" = "1" ]; then
        skipped "logout already revoked our tokens this run; clip cleanup is a manual step"
    else
        if ! curl_auth_json DELETE "$BACKEND_BASE_SYNC/audio/clips/${CLIP_ID}" >/dev/null; then
            failed "DELETE /audio/clips/${CLIP_ID} failed"
        fi
        ok "soft-deleted clip $CLIP_ID"
    fi
fi

# ---- step 18: optional Prometheus scrape ------------------------------------

step "Prometheus scrape ($OTEL_PROMETHEUS_URL)"
if curl --silent --show-error --max-time 5 -o /dev/null --fail "$OTEL_PROMETHEUS_URL" 2>/dev/null; then
    METRICS_BODY="$(curl --silent --max-time 5 "$OTEL_PROMETHEUS_URL")"
    printf 'OK\n'
    note "relevant counters:"
    for counter in \
        events_ingested_total \
        audio_clips_uploaded_total \
        audio_clips_downloaded_total \
        audio_clips_deleted_total \
        auth_attempts_total \
        refresh_attempts_total
    do
        # Print non-comment, non-blank lines that match the counter name.
        matched="$(printf '%s\n' "$METRICS_BODY" | grep -E "^${counter}(\\{|[[:space:]])" || true)"
        if [ -n "$matched" ]; then
            printf '       %s\n' "$matched" | sed 's/^/         /'
        else
            printf '         (no samples for %s)\n' "$counter"
        fi
    done
else
    skipped "$OTEL_PROMETHEUS_URL unreachable; pass --profile otel to docker compose to enable"
fi

printf '\nALL DONE: backend smoke complete.\n'
