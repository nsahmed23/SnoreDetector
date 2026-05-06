#!/usr/bin/env bash
# scripts/dev/export-smoke.sh
#
# Hits both export endpoints with all three ?include= variants (bare,
# sessions, sessions+clips) and pretty-prints what came back so you
# can sanity-check the wire shape.
#
# Inputs:
#   BEARER_TOKEN          (env, required)
#   BACKEND_BASE_EXPORT   (env, default http://localhost:8082)
#
# Flags:
#   --debug               print curl invocations (token masked)
#
# Refuses to run against non-localhost URLs.

set -euo pipefail

: "${BACKEND_BASE_EXPORT:=http://localhost:8082}"

DEBUG=0
while [ $# -gt 0 ]; do
    case "$1" in
        --debug) DEBUG=1; shift ;;
        -h|--help)
            sed -n '2,17p' "$0"
            exit 0
            ;;
        *) printf 'unknown flag: %s\n' "$1" >&2; exit 2 ;;
    esac
done

case "$BACKEND_BASE_EXPORT" in
    http://localhost*|http://127.0.0.1*|http://0.0.0.0*) ;;
    'http://[::1]'*) ;;
    http://host.docker.internal*) ;;
    *)
        printf 'refusing to run against non-localhost URL: %s\n' "$BACKEND_BASE_EXPORT" >&2
        exit 2
        ;;
esac

if [ -z "${BEARER_TOKEN:-}" ]; then
    printf 'BEARER_TOKEN env var is required.\n' >&2
    exit 2
fi

for bin in curl jq; do
    if ! command -v "$bin" >/dev/null 2>&1; then
        printf 'missing dependency: %s\n' "$bin" >&2
        exit 2
    fi
done

mask() {
    local s="${1:-}"
    if [ ${#s} -le 12 ]; then printf '<%d-char-redacted>' "${#s}"
    else printf '%s...%s' "${s:0:6}" "${s: -4}"; fi
}

step_n=0
total=6
step() { step_n=$((step_n + 1)); printf '[%d/%d] %s\n' "$step_n" "$total" "$1"; }
ok()   { printf '       OK%s\n' "${1:+ ($1)}"; }
fail() { printf '       FAIL %s\n' "$1" >&2; exit 1; }

# fetch_csv <include-spec> <out-file>
fetch_csv() {
    local include="$1"
    local out="$2"
    local url
    if [ -z "$include" ]; then
        url="$BACKEND_BASE_EXPORT/export/events.csv"
    else
        url="$BACKEND_BASE_EXPORT/export/events.csv?include=${include}"
    fi
    if [ "$DEBUG" = "1" ]; then
        printf 'DEBUG: GET %s Auth=Bearer %s\n' "$url" "$(mask "$BEARER_TOKEN")" >&2
    fi
    local info
    info="$(curl --silent --show-error --max-time 60 \
        -H "Authorization: Bearer ${BEARER_TOKEN}" \
        -o "$out" \
        -w '%{http_code} %{content_type}' \
        "$url")"
    printf '%s' "$info"
}

fetch_json() {
    local include="$1"
    local out="$2"
    local url
    if [ -z "$include" ]; then
        url="$BACKEND_BASE_EXPORT/export/events.json"
    else
        url="$BACKEND_BASE_EXPORT/export/events.json?include=${include}"
    fi
    if [ "$DEBUG" = "1" ]; then
        printf 'DEBUG: GET %s Auth=Bearer %s\n' "$url" "$(mask "$BEARER_TOKEN")" >&2
    fi
    local info
    info="$(curl --silent --show-error --max-time 60 \
        -H "Authorization: Bearer ${BEARER_TOKEN}" \
        -o "$out" \
        -w '%{http_code} %{content_type}' \
        "$url")"
    printf '%s' "$info"
}

CSV_BARE="$(mktemp)"
CSV_S="$(mktemp)"
CSV_SC="$(mktemp)"
JSON_BARE="$(mktemp)"
JSON_S="$(mktemp)"
JSON_SC="$(mktemp)"
trap 'rm -f "$CSV_BARE" "$CSV_S" "$CSV_SC" "$JSON_BARE" "$JSON_S" "$JSON_SC"' EXIT

# ---- CSV ---------------------------------------------------------------------

step "GET /export/events.csv (no include)"
INFO="$(fetch_csv "" "$CSV_BARE")"
STATUS="${INFO%% *}"
CT="${INFO#* }"
[ "$STATUS" = "200" ] || fail "status=$STATUS"
case "$CT" in text/csv*) : ;; *) fail "content-type=$CT" ;; esac
BYTES="$(wc -c < "$CSV_BARE")"
LINES="$(wc -l < "$CSV_BARE")"
# Bare export should NOT have section markers.
if grep -qF '# section:' "$CSV_BARE"; then
    fail "bare export unexpectedly contains section markers"
fi
ok "${BYTES} bytes, ${LINES} lines, no section markers (as expected)"

step "GET /export/events.csv?include=sessions"
INFO="$(fetch_csv "sessions" "$CSV_S")"
STATUS="${INFO%% *}"
[ "$STATUS" = "200" ] || fail "status=$STATUS"
BYTES="$(wc -c < "$CSV_S")"
for marker in "# section: events" "# section: sessions"; do
    grep -qF "$marker" "$CSV_S" || fail "missing marker '$marker'"
done
if grep -qF "# section: clips" "$CSV_S"; then
    fail "include=sessions unexpectedly contains '# section: clips'"
fi
ok "${BYTES} bytes, sections: events + sessions"

step "GET /export/events.csv?include=sessions,clips"
INFO="$(fetch_csv "sessions,clips" "$CSV_SC")"
STATUS="${INFO%% *}"
[ "$STATUS" = "200" ] || fail "status=$STATUS"
BYTES="$(wc -c < "$CSV_SC")"
for marker in "# section: events" "# section: sessions" "# section: clips"; do
    grep -qF "$marker" "$CSV_SC" || fail "missing marker '$marker'"
done
ok "${BYTES} bytes, sections: events + sessions + clips"

# ---- JSON --------------------------------------------------------------------

step "GET /export/events.json (no include)"
INFO="$(fetch_json "" "$JSON_BARE")"
STATUS="${INFO%% *}"
[ "$STATUS" = "200" ] || fail "status=$STATUS"
jq -e '.events | type == "array"' "$JSON_BARE" >/dev/null || fail "no .events[] array"
# Bare shape: count is a scalar.
COUNT_TYPE="$(jq -r '.count | type' "$JSON_BARE")"
if [ "$COUNT_TYPE" != "number" ]; then
    fail "expected .count to be a number for bare export, got $COUNT_TYPE"
fi
BYTES="$(wc -c < "$JSON_BARE")"
EV_LEN="$(jq -r '.events | length' "$JSON_BARE")"
COUNT="$(jq -r '.count' "$JSON_BARE")"
ok "${BYTES} bytes, events=$EV_LEN, count=$COUNT (scalar)"

step "GET /export/events.json?include=sessions"
INFO="$(fetch_json "sessions" "$JSON_S")"
STATUS="${INFO%% *}"
[ "$STATUS" = "200" ] || fail "status=$STATUS"
jq -e '.events | type == "array"' "$JSON_S" >/dev/null || fail "no .events[] array"
jq -e '.sessions | type == "array"' "$JSON_S" >/dev/null || fail "no .sessions[] array"
# With include=, count is an object.
jq -e '.count | type == "object"' "$JSON_S" >/dev/null || \
    fail "expected .count to be an object for include= export"
BYTES="$(wc -c < "$JSON_S")"
ok "${BYTES} bytes, count map: $(jq -c '.count' "$JSON_S")"

step "GET /export/events.json?include=sessions,clips"
INFO="$(fetch_json "sessions,clips" "$JSON_SC")"
STATUS="${INFO%% *}"
[ "$STATUS" = "200" ] || fail "status=$STATUS"
jq -e '.events  | type == "array"' "$JSON_SC" >/dev/null || fail "no .events[] array"
jq -e '.sessions| type == "array"' "$JSON_SC" >/dev/null || fail "no .sessions[] array"
jq -e '.clips   | type == "array"' "$JSON_SC" >/dev/null || fail "no .clips[] array"
jq -e '.count   | type == "object"' "$JSON_SC" >/dev/null || fail "no .count{} map"
BYTES="$(wc -c < "$JSON_SC")"
ok "${BYTES} bytes, count map: $(jq -c '.count' "$JSON_SC")"

printf '\nDONE: export-smoke complete.\n'
