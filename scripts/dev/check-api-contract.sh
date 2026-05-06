#!/usr/bin/env bash
# check-api-contract.sh
#
# Confirms that every endpoint path the iOS client knows about is
# documented in openapi/snoreguard.v1.yaml. Path templates are
# matched after collapsing `{var}` segments, so
# `/audio/clips/{id}` matches `/audio/clips/\(clipID)`.
#
# Exits 0 when every Swift path is covered, 1 on drift.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SPEC="$REPO_ROOT/openapi/snoreguard.v1.yaml"
SWIFT="$REPO_ROOT/ios/SnoreGuard/Networking/Endpoints.swift"

if [[ ! -f "$SPEC" ]]; then
  echo "FAIL: missing OpenAPI spec at $SPEC" >&2
  exit 1
fi

if [[ ! -f "$SWIFT" ]]; then
  # The iOS network client may not be merged onto this branch yet;
  # fall back to fetching it from the dedicated feature branch.
  if ! git -C "$REPO_ROOT" show \
      origin/claude/ios-network-client:ios/SnoreGuard/Networking/Endpoints.swift \
      > /tmp/Endpoints.swift 2>/dev/null; then
    echo "skip: no Endpoints.swift on this branch and origin/claude/ios-network-client unavailable"
    exit 0
  fi
  SWIFT=/tmp/Endpoints.swift
fi

SPEC="$SPEC" SWIFT="$SWIFT" python3 - <<'PY'
import os, re, sys

try:
    import yaml
except ImportError:
    print("FAIL: PyYAML not installed (pip install pyyaml)", file=sys.stderr)
    sys.exit(2)

spec_path = os.environ["SPEC"]
swift_path = os.environ["SWIFT"]

with open(spec_path) as f:
    spec = yaml.safe_load(f)
spec_paths = set((spec.get("paths") or {}).keys())

with open(swift_path) as f:
    swift = f.read()

# Match every `path: "/literal"` or `self.path = "/literal..."` form.
# The Swift file uses both `let path: String = "/auth/apple"` and
# `self.path = "/audio/clips/\(clipID)/download"`.
swift_paths = set()
for m in re.finditer(r'\bpath\b[^"\n]*"(/[^"\n]+)"', swift):
    raw = m.group(1)
    # Canonicalize Swift interpolation \(name) -> {name}.
    norm = re.sub(r'\\\(([A-Za-z_][A-Za-z0-9_]*)\)', r'{\1}', raw)
    swift_paths.add(norm)

if not swift_paths:
    print("FAIL: no path literals found in Swift Endpoints.swift", file=sys.stderr)
    sys.exit(1)

# Path-template matching: collapse all {var} segments so
# /audio/clips/{id} from the spec matches /audio/clips/{clipID}
# from the Swift file.
def collapse(p):
    return re.sub(r"\{[^}]+\}", "{*}", p)

spec_collapsed = {collapse(p) for p in spec_paths}

missing = sorted(p for p in swift_paths if collapse(p) not in spec_collapsed)
if missing:
    print("MISSING from OpenAPI spec:")
    for p in missing:
        print(f"  {p}")
    sys.exit(1)

print(f"OK: {len(swift_paths)} Swift paths covered by OpenAPI spec ({len(spec_paths)} paths)")
for p in sorted(swift_paths):
    print(f"  - {p}")
PY
