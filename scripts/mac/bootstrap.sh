#!/usr/bin/env bash
# Bootstrap a fresh macOS environment for SnoreGuard development.
# Idempotent — safe to run multiple times.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "error: bootstrap.sh must run on macOS (uses xcodebuild, xcodegen, etc.)" >&2
  exit 1
fi

# 1. xcodegen
if ! command -v xcodegen >/dev/null; then
  echo "Installing xcodegen via Homebrew..."
  brew install xcodegen
fi

# 2. rustup + iOS targets
if ! command -v rustup >/dev/null; then
  echo "Installing rustup..."
  curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable
  # shellcheck disable=SC1091
  source "$HOME/.cargo/env"
fi
rustup target add aarch64-apple-ios aarch64-apple-ios-sim x86_64-apple-ios

# 3. Validate project.yml
python3 -c "import yaml; yaml.safe_load(open('ios/project.yml'))"

# 4. Build the xcframework
cd rust-core
./scripts/build-xcframework.sh
cd "$REPO_ROOT"

# 5. Generate the Xcode project
cd ios
xcodegen generate
cd "$REPO_ROOT"

echo "Bootstrap complete. Open ios/SnoreGuard.xcodeproj in Xcode."
