# ADR 0005: Cloud sync and raw audio upload in v1

## Status

Accepted.

## Context

The minimum viable scope for "snoring tracker that runs on iPhone" is a
local-only app: record, detect, save events to local storage, show
graphs. That MVP fits in a single binary and avoids the trust surface of
a backend.

The product target for v1 is broader: a portfolio-grade end-to-end
mobile platform that demonstrates the full stack — native mobile,
systems (Rust), services (Go), object storage, telemetry, privacy
controls. Local-only would not exercise those layers.

The hardest sub-decision inside this scope is **raw audio**. Bedroom
audio is sensitive. We must not store more of it on a server than the
user has actively consented to, and we must not store any of it
continuously.

## Decision

Include in v1:

- **Cloud sync** of recording sessions and snore-like event metadata.
  Off by default. Single Settings toggle. When on, every new session
  and event is pushed to the user's account on the backend, and a
  second device on the same Apple account pulls the same data via
  cursor pagination.
- **Raw audio clip upload**. Off by default. Independent toggle, only
  visible when cloud sync is on. When on, **only the short clips around
  detected events** are uploaded. Continuous, all-night raw audio is
  never uploaded. Clips are sha256-verified at the boundary, stored
  under opaque UUID keys, and soft-deletable per-clip via API.

## Consequences

**Positive**:

- Demonstrates full-stack engineering: HTTP transport + auth + token
  rotation + object storage + retention + delete.
- Multi-device experience works: a user can install the app on their
  iPad and see history that was recorded on their iPhone.
- The double-opt-in (cloud sync ON + audio upload ON) means raw audio
  upload is impossible without two affirmative actions.
- Per-clip soft-delete + retention policy give the user surgical
  control.

**Negative**:

- A backend exists, which means it can fail, leak, or be misconfigured.
  Mitigated by: rate limits, refresh-token rotation with theft
  detection, hashed user IDs in telemetry, no raw audio in
  spans/logs/exports, a documented threat model.
- Raw audio in object storage is a high-impact data class. Mitigated
  by: the double opt-in, opaque UUID keys, sha256 verification, soft-
  delete + retention, and a policy that the operator must enforce
  encryption-at-rest at the bucket level (deploy-target-dependent).
- The app is no longer a "single-binary, no-network" experience.
  Mitigated by: cloud sync is strictly optional; the app remains fully
  functional offline.

**Neutral**:

- Increases the test matrix substantially. We add API contract tests,
  fake-blobstore tests, and integration tests that require a real
  Postgres + filesystem object store.

## Alternatives considered

- **Local-only v1**. Rejected: doesn't demonstrate the backend,
  observability, privacy-controls-on-server, refresh-token rotation, or
  the object-storage surface — all of which are first-class portfolio
  signals. Reduces the project to "iOS app that runs an FFT".
- **Sync metadata only, no raw audio**. Rejected: raw audio upload is
  the most interesting trust-engineering surface in the app (sha256
  verification, soft-delete, retention, double opt-in, no-trace
  rules). Skipping it would remove the most meaningful security and
  privacy work. The user-facing value also matters: clip review on a
  second device requires the clip to be stored somewhere accessible
  from a second device, which is exactly what cloud upload provides.
