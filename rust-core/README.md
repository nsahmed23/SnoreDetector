# rust-core/

Rust algorithm core for SnoreGuard. The `snoreguard-core` crate ports the
heuristic detector from the React prototype's `useAudioMonitor` hook into a
self-contained Rust library that exposes a stable C ABI for the Swift app to
call.

## Layout

```
rust-core/
├── snoreguard-core/         The Cargo crate
│   ├── Cargo.toml
│   ├── build.rs             Regenerates include/snoreguard_core.h via cbindgen
│   ├── cbindgen.toml
│   ├── src/
│   │   ├── lib.rs           Crate root, public re-exports
│   │   ├── detector.rs      Heuristic detector + unit tests
│   │   └── ffi.rs           C-ABI surface
│   ├── include/
│   │   └── snoreguard_core.h   Generated header (committed)
│   └── tests/
│       └── integration.rs   FFI lifecycle + property tests
└── scripts/
    └── build-xcframework.sh    Builds SnoreGuardCore.xcframework on macOS
```

## What it does

The crate accepts mono `f32` audio frames (256 samples each), runs a real FFT
via `rustfft`, and applies the prototype's heuristic:

1. Compute the average FFT magnitude → derive an uncalibrated dB-ish value in
   `[30, 100]` (matches the prototype's range so existing UI thresholds remain
   valid).
2. Compare the energy in the lower 1/4 of the spectrum to the upper 3/4. The
   ratio threshold depends on `SgSensitivity` (`Low = 2.0×`, `Medium = 1.5×`,
   `High = 1.2×`).
3. A frame is **snore-like** when its dB crosses the configured threshold AND
   low frequencies dominate.
4. An **event** starts after the snore-like condition holds for
   `DEFAULT_SUSTAIN_FRAMES` (15) consecutive frames, and ends as soon as the
   condition breaks.

This is a heuristic — there is no machine-learning model. See
[`docs/DISCLAIMER.md`](../docs/DISCLAIMER.md) for the algorithm's known
failure modes.

## Building (host)

```sh
cd snoreguard-core
cargo test                  # 13 tests across unit + FFI integration
cargo build --release       # produces target/release/libsnoreguard_core.a
```

`cargo build` also regenerates `include/snoreguard_core.h` via `build.rs`. The
header is checked into git so Swift consumers don't need cargo installed.

## Building for iOS (xcframework)

The Rust iOS targets are **Tier 2 without host tools** in the [Rust target
support tiers](https://doc.rust-lang.org/rustc/platform-support.html), so
cross-compilation requires the iOS SDK from Xcode. The build script must run
on macOS.

```sh
# One-time setup
rustup target add aarch64-apple-ios aarch64-apple-ios-sim x86_64-apple-ios

# Build the xcframework
./scripts/build-xcframework.sh
# → rust-core/SnoreGuardCore.xcframework
```

The script:

1. Builds three `cargo build --release` slices (device, Apple-Silicon
   simulator, Intel simulator).
2. `lipo`-fuses the two simulator slices.
3. Stages the generated header + a `module.modulemap` so Swift can `import
   SnoreGuardCore`.
4. Calls `xcodebuild -create-xcframework`.

Drop `SnoreGuardCore.xcframework` into the Xcode project as a binary
dependency (linked + embedded "Do not embed" since it's a static library).

## C ABI

| Function | Purpose |
|---|---|
| `sg_detector_new` | Allocate a detector. Returns NULL on `sample_rate_hz == 0`. |
| `sg_detector_free` | Free a detector (NULL-safe). |
| `sg_detector_push_frame` | Push exactly 256 `f32` samples. Returns `SgStatus::EventReady` (1) when an event was just finalized. |
| `sg_detector_poll_event` | Drain the most-recently-finalized event. |
| `sg_detector_set_threshold` | Update threshold dB at runtime. |
| `sg_detector_set_sensitivity` | Update sensitivity at runtime. |
| `sg_frame_size` | Returns `FRAME_SIZE` (256). |

Threading: the detector is **not** `Sync`. Confine all calls to one thread or
serialize via a queue. The expected pattern (per
[`docs/NATIVE_IOS_PORT_PLAN.md`](../docs/NATIVE_IOS_PORT_PLAN.md)) is the
audio render thread pushing frames; the UI thread polls events on a 30 Hz
timer.

## Out of scope (next phases)

- Swift wrapper (`SnoreCore.swift`) — phase 2.
- Real `AVAudioEngine` integration — phase 2.
- Property tests at higher coverage (e.g., `proptest` strategies for
  arbitrary frame mixes).
- A SIMD-accelerated path using `Accelerate` instead of `rustfft` — only
  worth doing if profiling shows FFT is the bottleneck.
