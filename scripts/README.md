# scripts/

Helper scripts that aren't part of the build graph for any one service.

Scripts in `mac/` are macOS-only — they invoke `xcodebuild`, `xcodegen`,
and the iOS SDK, and they refuse to run on non-Darwin hosts. Linux CI
verifies syntax (`bash -n`) and runs `shellcheck` when available, but
does not execute them. End-to-end verification of the macOS scripts
happens on a Mac with Xcode 15.4 or newer (see `docs/MAC_QUICKSTART.md`).
