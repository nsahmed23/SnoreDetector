#!/usr/bin/env bash
# Build SnoreGuardCore.xcframework for iOS device + simulator.
#
# Requirements:
#   - Xcode + iOS SDK (script must run on macOS)
#   - rustup with the three iOS targets installed:
#       rustup target add aarch64-apple-ios aarch64-apple-ios-sim x86_64-apple-ios
#   - cargo
#
# Output: rust-core/SnoreGuardCore.xcframework
#
# These targets are Tier 2 in the Rust target tier table — supported,
# but cross-compilation requires the Xcode iOS SDK at build time.
# https://doc.rust-lang.org/rustc/platform-support.html

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
RUST_CORE_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
CRATE_DIR="$RUST_CORE_DIR/snoreguard-core"
OUT_DIR="$RUST_CORE_DIR/SnoreGuardCore.xcframework"
HEADERS_DIR="$RUST_CORE_DIR/build/headers"
SIM_DIR="$RUST_CORE_DIR/build/sim"

PROFILE="${PROFILE:-release}"
LIB_NAME="libsnoreguard_core.a"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "error: this script must run on macOS (requires xcodebuild and the iOS SDK)" >&2
  exit 1
fi

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: required tool '$1' is not on PATH" >&2
    exit 1
  fi
}
require cargo
require xcodebuild
require lipo
require xcrun

cd "$CRATE_DIR"

echo ">> Building aarch64-apple-ios (device)"
SDKROOT=$(xcrun --sdk iphoneos --show-sdk-path) \
  cargo build --"$PROFILE" --target aarch64-apple-ios

echo ">> Building aarch64-apple-ios-sim (Apple Silicon simulator)"
SDKROOT=$(xcrun --sdk iphonesimulator --show-sdk-path) \
  cargo build --"$PROFILE" --target aarch64-apple-ios-sim

echo ">> Building x86_64-apple-ios (Intel simulator)"
SDKROOT=$(xcrun --sdk iphonesimulator --show-sdk-path) \
  cargo build --"$PROFILE" --target x86_64-apple-ios

echo ">> Lipoing simulator slices"
mkdir -p "$SIM_DIR"
lipo -create \
  "target/aarch64-apple-ios-sim/$PROFILE/$LIB_NAME" \
  "target/x86_64-apple-ios/$PROFILE/$LIB_NAME" \
  -output "$SIM_DIR/$LIB_NAME"

echo ">> Staging headers"
rm -rf "$HEADERS_DIR"
mkdir -p "$HEADERS_DIR"
cp "$CRATE_DIR/include/snoreguard_core.h" "$HEADERS_DIR/"
cat > "$HEADERS_DIR/module.modulemap" <<'EOF'
module SnoreGuardCore {
    header "snoreguard_core.h"
    export *
}
EOF

echo ">> Assembling xcframework"
rm -rf "$OUT_DIR"
xcodebuild -create-xcframework \
  -library "$CRATE_DIR/target/aarch64-apple-ios/$PROFILE/$LIB_NAME" \
    -headers "$HEADERS_DIR" \
  -library "$SIM_DIR/$LIB_NAME" \
    -headers "$HEADERS_DIR" \
  -output "$OUT_DIR"

echo
echo "✅ Built $OUT_DIR"
