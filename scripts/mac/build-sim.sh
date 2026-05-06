#!/usr/bin/env bash
set -euo pipefail
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT/ios"

if [[ ! -d SnoreGuard.xcodeproj ]]; then
  echo "SnoreGuard.xcodeproj missing — run scripts/mac/bootstrap.sh first." >&2
  exit 1
fi

DESTINATION="${DESTINATION:-platform=iOS Simulator,name=iPhone 15 Pro}"

xcodebuild build \
  -project SnoreGuard.xcodeproj \
  -scheme SnoreGuard \
  -destination "$DESTINATION" \
  -configuration Debug \
  CODE_SIGNING_REQUIRED=NO CODE_SIGNING_ALLOWED=NO
