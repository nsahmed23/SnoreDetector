# ADR 0002: Use Rust for the detector core

## Status

Accepted.

## Context

The snore-like-event detector is a deterministic heuristic: FFT a 256-
sample frame, compare lower-quarter spectrum energy to the rest, count
sustained frames, emit an event. We need:

- Real-time-safe execution on the audio render thread (no GC pauses, no
  allocations per frame).
- Deterministic output — given the same audio, the same events.
  Required for property tests and CI on Linux runners that don't have
  Mac or iPhone hardware.
- A clean ABI to call from Swift (the iOS app) and potentially from
  other host languages later (Go for offline batch reprocessing, a Zig
  CLI for audio-lab benchmarks).
- Memory safety. The detector handles untrusted audio buffers; no
  buffer overruns allowed.

## Decision

Implement the detector as a Rust crate (`rust-core/snoreguard-core`)
built as a `staticlib` with a hand-curated C ABI surface (see
`rust-core/README.md`). Package as `SnoreGuardCore.xcframework` for iOS
device + Apple-Silicon simulator + Intel simulator slices.

## Consequences

**Positive**:

- Memory safety on the most security-sensitive code path (raw audio
  ingest from microphone).
- The exact same crate runs on Mac, Linux, and iOS — Linux CI runners
  exercise the unit + property + FFI lifecycle tests on every push,
  without needing a Mac in CI.
- Rich crate ecosystem (`rustfft`, `proptest`).
- Opaque-pointer C ABI is easy to import into Swift via a bridging
  header and is portable to Go cgo + Zig if we ever need it.

**Negative**:

- Rust iOS targets are Tier-2-without-host-tools — cross-compilation
  requires the iOS SDK, which only ships with Xcode on macOS. The
  xcframework build script must run on a Mac (CI or developer
  workstation).
- Adds a Rust toolchain prerequisite for full local builds. Mitigated:
  the generated header (`include/snoreguard_core.h`) is checked into
  git, so Swift consumers without `cargo` installed can still read the
  ABI surface.
- One extra build phase in Xcode.

**Neutral**:

- The crate is std-only for v1; no SIMD path. We add `Accelerate` /
  `vDSP` only if profiling shows FFT is the bottleneck, which it is not
  expected to be at 16 kHz with 256-sample frames.

## Alternatives considered

- **Pure Swift detector**. Rejected: would need to be re-written in a
  separate Swift package to be testable on Linux CI (Swift on Linux
  works for CLI but Apple frameworks don't). And we get no portability
  to Go or Zig host bindings later. We would also lose the property
  tests we want to run on every PR.
- **Swift + `Accelerate.vDSP_fft_zrip`**. Rejected: same Linux-CI
  problem (Accelerate is Apple-only), no host-language portability, and
  the `vDSP` API is C-flavoured anyway — we would not actually save
  much code. Worth revisiting only if the Rust implementation is
  measurably slow on real hardware.
- **C / C++**. Rejected: no memory safety on the audio buffer path, no
  built-in test harness as ergonomic as `cargo test`, and no
  property-test crate as widely used as `proptest`. C interop would
  match the Rust ABI but the safety story is strictly worse.
