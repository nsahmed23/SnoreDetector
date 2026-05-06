# ADR 0007: Use Zig only for audio-lab tooling until production justifies it

## Status

Accepted.

## Context

Zig is appealing for systems programming work — small toolchain, simple
build system, painless cross-compilation, good C interop. It is
attractive for offline audio benchmarks and DSP experiments where we
want to compare detector variants quickly, batch-process recorded WAVs,
or generate synthetic test audio.

It is not yet justified inside the iOS production path. The detector
core is already in Rust (per ADR 0002) with full FFI lifecycle tests,
property tests, and a stable C ABI. Replacing or augmenting Rust on the
audio render thread with Zig would require:

- A second toolchain in CI.
- A second xcframework build pipeline.
- Re-deriving the same property-test coverage in a different language.
- An ergonomic-bindings story for Swift.

None of that is paid back unless Zig demonstrably outperforms Rust on
the iOS audio path or unlocks a feature we cannot get otherwise.

## Decision

Zig has **no role in the iOS production binary** for v1. Zig is reserved
for an offline audio-lab CLI (`claude/zig-audio-lab` branch, future
work) used only on the developer's workstation for:

- Batch-processing WAV files for detector tuning.
- Generating synthetic snore-like and non-snore-like audio fixtures.
- Comparative benchmarking of detector variants.

If profiling on real iOS hardware ever shows the Rust detector is the
bottleneck and a Zig variant would unblock something specific, we
revisit this ADR with measurements attached.

## Consequences

**Positive**:

- One language on the iOS audio path: Rust. One ABI surface.
- CI stays Rust + Swift + Go.
- Zig still gets to exist in the project as a tooling story that is
  honest about what it is for.

**Negative**:

- Zig fans on the recruiter side might wonder why it isn't on the iOS
  critical path. Mitigated by `PORTFOLIO_NARRATIVE.md`'s "why each
  language" section, which calls this out explicitly.

**Neutral**:

- The audio-lab CLI is a deferred branch. It is not blocked by this
  ADR; it is simply scoped out of v1.

## Alternatives considered

- **Cram Zig into the iOS production path now (replace or augment Rust
  in the detector)**. Rejected: no measured performance need. Rust's
  property + FFI lifecycle test coverage is already in place;
  re-implementing in Zig means re-deriving that coverage. The build +
  CI complexity goes up; the user-visible behaviour does not.
- **Skip Zig entirely**. Rejected: Zig genuinely is great for offline
  DSP work and synthetic-fixture generation, and the audio-lab CLI is a
  natural place for it. Removing the option closes a useful door for
  no real saving.
