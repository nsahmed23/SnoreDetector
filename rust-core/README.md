# rust-core/

Rust algorithm core for SnoreGuard. Empty stub for now — the `snoreguard-core`
crate lands in a follow-up branch (`rust-core-skeleton`, phase 1 in
`docs/NATIVE_IOS_PORT_PLAN.md`).

Built as a `staticlib` and packaged into `SnoreGuardCore.xcframework`. Targets:

- `aarch64-apple-ios` — real iPhone / iPad
- `aarch64-apple-ios-sim` — Apple-Silicon simulator
- `x86_64-apple-ios` — Intel-Mac simulator

Per the Rust target tier docs, these are **Tier 2 without host tools** —
cross-compilation requires the iOS SDK from Xcode. See the port plan for the
full build pipeline (cargo + cbindgen + `xcodebuild -create-xcframework`).
