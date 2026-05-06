#!/usr/bin/env bash
set -euo pipefail
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT/ios"

DESTINATION="${DESTINATION:-platform=iOS Simulator,name=iPhone 15 Pro}"

xcodebuild test \
  -project SnoreGuard.xcodeproj \
  -scheme SnoreGuard \
  -destination "$DESTINATION" \
  -configuration Debug \
  CODE_SIGNING_REQUIRED=NO CODE_SIGNING_ALLOWED=NO
