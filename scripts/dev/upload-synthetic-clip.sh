#!/usr/bin/env bash
# scripts/dev/upload-synthetic-clip.sh
#
# Upload one synthetic audio clip to the local sync-service. Useful
# when iterating on the clip endpoint specifically.
#
# Inputs:
#   BEARER_TOKEN          (env, required) — access token from /auth/apple
#   BACKEND_BASE_SYNC     (env, default http://localhost:8080)
#
# Flags:
#   --size <bytes>        synthetic-clip body size (default 4096)
#   --mime  <type>        Content-Type for the file part (default audio/m4a)
#   --session-id <uuid>   pre-existing remote session_id; if absent,
#                         POST /sessions first to mint one.
#   --debug               print the curl command (token masked)
#
# Refuses to run against non-localhost URLs.

set -euo pipefail

: "${BACKEND_BASE_SYNC:=http://localhost:8080}"

SIZE=4096
MIME="audio/m4a"
SESSION_ID=""
DEBUG=0

while [ $# -gt 0 ]; do
    case "$1" in
        --size)       SIZE="$2"; shift 2 ;;
        --mime)       MIME="$2"; shift 2 ;;
        --session-id) SESSION_ID="$2"; shift 2 ;;
        --debug)      DEBUG=1; shift ;;
        -h|--help)
            sed -n '2,21p' "$0"
            exit 0
            ;;
        *) printf 'unknown flag: %s\n' "$1" >&2; exit 2 ;;
    esac
done

# Localhost guard.
case "$BACKEND_BASE_SYNC" in
    http://localhost*|http://127.0.0.1*|http://0.0.0.0*) ;;
    'http://[::1]'*) ;;
    http://host.docker.internal*) ;;
    *)
        printf 'refusing to run against non-localhost URL: %s\n' "$BACKEND_BASE_SYNC" >&2
        exit 2
        ;;
esac

if [ -z "${BEARER_TOKEN:-}" ]; then
    printf 'BEARER_TOKEN env var is required.\n' >&2
    exit 2
fi

case "$SIZE" in
    ''|*[!0-9]*) printf '--size must be a positive integer, got: %s\n' "$SIZE" >&2; exit 2 ;;
esac
if [ "$SIZE" -lt 1 ] || [ "$SIZE" -gt 5242880 ]; then
    printf '--size must be in [1, 5242880]\n' >&2
    exit 2
fi

for bin in curl jq python3; do
    if ! command -v "$bin" >/dev/null 2>&1; then
        printf 'missing dependency: %s\n' "$bin" >&2
        exit 2
    fi
done

step_n=0
total=4
step() { step_n=$((step_n + 1)); printf '[%d/%d] %s ... ' "$step_n" "$total" "$1"; }
ok()   { printf 'OK%s\n' "${1:+ ($1)}"; }
fail() { printf 'FAIL\n       %s\n' "$1" >&2; exit 1; }

mask() {
    local s="${1:-}"
    if [ ${#s} -le 12 ]; then printf '<%d-char-redacted>' "${#s}"
    else printf '%s...%s' "${s:0:6}" "${s: -4}"; fi
}

gen_uuid()    { python3 -c 'import uuid; print(uuid.uuid4())'; }
rfc3339_now() { python3 -c 'from datetime import datetime,timezone; print(datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))'; }

# ---- step 1: ensure session --------------------------------------------------

step "ensure remote session_id"
if [ -z "$SESSION_ID" ]; then
    CLIENT_SESSION_ID="$(gen_uuid)"
    STARTED_AT="$(rfc3339_now)"
    SESS_BODY="$(jq -n --arg cid "$CLIENT_SESSION_ID" --arg s "$STARTED_AT" \
        '{client_session_id:$cid, started_at:$s, device_name:"upload-synthetic-clip", app_version:"0.0.0-smoke"}')"
    if [ "$DEBUG" = "1" ]; then
        printf '\nDEBUG: POST %s/sessions Auth=Bearer %s\n' "$BACKEND_BASE_SYNC" "$(mask "$BEARER_TOKEN")" >&2
    fi
    SESS_RESP="$(curl --silent --show-error --max-time 30 --fail-with-body \
        -X POST \
        -H "Authorization: Bearer ${BEARER_TOKEN}" \
        -H 'Content-Type: application/json' \
        --data-raw "$SESS_BODY" \
        "$BACKEND_BASE_SYNC/sessions")" || fail "POST /sessions failed"
    SESSION_ID="$(echo "$SESS_RESP" | jq -r '.session_id // empty')"
    if [ -z "$SESSION_ID" ]; then
        fail "POST /sessions returned no session_id"
    fi
    ok "minted new session $SESSION_ID"
else
    ok "using provided $SESSION_ID"
fi

# ---- step 2: generate body + sha256 -----------------------------------------

step "generate ${SIZE}-byte synthetic clip + sha256"
BODY_HASH_FILE="$(mktemp)"
trap 'rm -f "$BODY_HASH_FILE" "$RESP_FILE"' EXIT

# Stash the hash; the body itself stays in python's memory and is
# piped into curl on stdin without ever touching the filesystem.
SIZE="$SIZE" python3 - <<'PYEOF' >"$BODY_HASH_FILE"
import hashlib, os, secrets, sys
n = int(os.environ["SIZE"])
b = secrets.token_bytes(n)
sys.stdout.write(hashlib.sha256(b).hexdigest())
PYEOF
LOCAL_SHA="$(cat "$BODY_HASH_FILE")"
ok "sha256=${LOCAL_SHA:0:8}..."

# ---- step 3: build multipart envelope and POST ------------------------------

step "POST /audio/clips (multipart, ${MIME})"
CLIP_CLIENT_ID="$(gen_uuid)"
STARTED_AT="$(rfc3339_now)"
META_JSON="$(jq -n \
    --arg cid "$CLIP_CLIENT_ID" \
    --arg sid "$SESSION_ID" \
    --arg sa  "$STARTED_AT" \
    --arg sha "$LOCAL_SHA" \
    '{client_clip_id:$cid, session_id:$sid, started_at:$sa, duration_ms:1500, avg_db:62.5, sha256:$sha}')"

RESP_FILE="$(mktemp)"
trap 'rm -f "$BODY_HASH_FILE" "$RESP_FILE"' EXIT

# Python heredoc builds + uploads the multipart envelope in-memory.
# This avoids bash's painful binary-multipart construction.
BACKEND_BASE_SYNC="$BACKEND_BASE_SYNC" \
BEARER_TOKEN="$BEARER_TOKEN" \
META_JSON="$META_JSON" \
SIZE="$SIZE" \
MIME="$MIME" \
RESP_FILE="$RESP_FILE" \
DEBUG="$DEBUG" \
python3 - <<'PYEOF' || fail "multipart upload failed"
import hashlib, json, os, secrets, subprocess, sys, uuid

base    = os.environ["BACKEND_BASE_SYNC"]
token   = os.environ["BEARER_TOKEN"]
meta    = json.loads(os.environ["META_JSON"])
size    = int(os.environ["SIZE"])
mime    = os.environ["MIME"]
respfp  = os.environ["RESP_FILE"]
debug   = os.environ.get("DEBUG", "0") == "1"

# Re-generate the same-shape random body. (Hash was generated above
# from a different random buffer; we deterministically pin them here.)
body = secrets.token_bytes(size)
sha  = hashlib.sha256(body).hexdigest()
meta["sha256"] = sha  # pin to THIS body, overriding the staged hash

boundary = "----snoreguard-upload-" + uuid.uuid4().hex
crlf = b"\r\n"
parts = [
    f"--{boundary}".encode(),
    b'Content-Disposition: form-data; name="metadata"',
    b'Content-Type: application/json',
    b"",
    json.dumps(meta).encode(),
    f"--{boundary}".encode(),
    b'Content-Disposition: form-data; name="file"; filename="clip.bin"',
    f'Content-Type: {mime}'.encode(),
    b"",
    body,
    f"--{boundary}--".encode(),
    b"",
]
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
    masked = (token[:6] + "..." + token[-4:]) if len(token) > 12 else "<masked>"
    sys.stderr.write(
        f"DEBUG: curl POST {base}/audio/clips multipart "
        f"({len(envelope)} bytes envelope, {size} bytes body, "
        f"sha256={sha[:8]}..., Auth=Bearer {masked})\n"
    )
proc = subprocess.run(cmd, input=envelope, capture_output=True)
with open(respfp, "wb") as f:
    f.write(proc.stdout)
# Print the hash we actually uploaded so the bash side can confirm.
sys.stderr.write(f"__UPLOADED_SHA256__:{sha}\n")
if proc.returncode != 0:
    sys.stderr.write(proc.stderr.decode(errors="replace"))
    sys.exit(proc.returncode)
PYEOF

RESP="$(cat "$RESP_FILE")"
CLIP_ID="$(echo "$RESP" | jq -r '.clip_id // empty')"
OBJECT_KEY="$(echo "$RESP" | jq -r '.object_key // empty')"
UPLOADED_AT="$(echo "$RESP" | jq -r '.uploaded_at // empty')"
CREATED="$(echo "$RESP" | jq -r '.created // empty')"

if [ -z "$CLIP_ID" ]; then
    fail "POST /audio/clips returned no clip_id (got: $RESP)"
fi
ok "clip_id=$CLIP_ID created=$CREATED"

# ---- step 4: report ---------------------------------------------------------

step "result"
ok ""
printf '       clip_id     %s\n' "$CLIP_ID"
printf '       object_key  %s\n' "$OBJECT_KEY"
printf '       uploaded_at %s\n' "$UPLOADED_AT"
printf '       created     %s\n' "$CREATED"
printf '       size_bytes  %s\n' "$SIZE"
printf '       mime        %s\n' "$MIME"
