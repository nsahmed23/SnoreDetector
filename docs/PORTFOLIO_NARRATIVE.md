# SnoreGuard — portfolio narrative

A one-page recruiter-facing summary of the SnoreGuard project. For the
deeper technical docs, see [`ARCHITECTURE.md`](ARCHITECTURE.md), the
[ADRs](ADRs/), [`THREAT_MODEL.md`](THREAT_MODEL.md), and
[`OBSERVABILITY.md`](OBSERVABILITY.md).

---

## One-paragraph project pitch

**SnoreGuard** is an end-to-end mobile platform for tracking snore-like
events during sleep. It is built as a native iOS app (SwiftUI) backed
by an on-device Rust detector core (FFT heuristic, packaged as an
xcframework over a stable C ABI), three Go backend services
(`sync-service`, `analytics-service`, `export-service`), HealthKit
read/write across three opt-in toggles, opt-in cloud sync of recording
sessions and snore-like events, opt-in raw-audio-clip upload (sha256-
verified, soft-delete, retention), full OpenTelemetry instrumentation
across the backend, structured privacy controls, and CI on every push.
It was built solo as a portfolio project to demonstrate full-stack
engineering across native mobile, systems programming, services, and
operations — in a single shippable product, not a stack of unrelated
exercises.

---

## Architecture highlights

- **Clean boundaries**. iOS app talks to the Rust core over a stable C
  ABI (`SnoreGuardCore.xcframework`), and to the backend over HTTP.
  Backend services emit OTLP. There are no fuzzy abstractions; every
  cross-process boundary is explicit and tested.
- **Typed compound-cursor pagination**. `GET /events` uses a
  `(received_at, id)` cursor that survives same-timestamp batches —
  the original single-timestamp cursor silently dropped rows when an
  iOS client uploaded a multi-event batch in one transaction. The fix
  threads the same cursor through the export streaming loops too.
- **Refresh-token rotation with theft detection**. Every `/auth/refresh`
  rotates the token. Reusing a previously-rotated refresh token
  triggers revocation of the entire `family_id`, bounding blast radius
  if a refresh token leaks.
- **Opt-in raw audio with sha256 + soft-delete + retention**. Audio
  upload is a double opt-in (cloud sync ON + audio upload ON, both
  default off). Each clip is sha256-verified at the boundary, stored
  under an opaque UUID key, soft-deletable, and pruned per retention.

---

## Security highlights

- **HS256 access + refresh tokens with rotation and revocation**. Access
  TTL 1 h, refresh TTL 60 d, rotated on use, revoked-jti table
  consulted on every authenticated request.
- **Apple identity-token verification with algorithm allowlist**. The
  Apple verifier accepts only `RS256` / `ES256`; the session-token
  verifier accepts only `HS256`. Algorithm-confusion forging (HS256
  presented as RS256) and `alg=none` are both rejected.
- **Per-user export budget + per-IP rate limit**. Bounds amplification
  attacks from a single compromised account.
- **Hashed user IDs in telemetry**. `metrics.HashUserID(uid)` →
  `sha256(uid)[:8]`. Raw UUIDs / Apple `sub` / emails / tokens are
  never present in spans or logs.
- **No raw audio anywhere observable**. Audio bytes never appear in
  spans, metrics, logs, or exports. Clip downloads go through a
  separate authenticated endpoint.

---

## Reliability + observability

- **OpenTelemetry across three services**. OTLP HTTP transport. Span
  hierarchy with handler → store → JWT-verify → blobstore-op spans.
- **Prometheus scrape endpoint** on `:9464` for local dev via the
  bundled OTel collector (debug exporter to stdout + Prometheus
  exporter).
- **Structured logs** via Go `slog`, JSON output, level via
  `LOG_LEVEL`.
- **Integration tests against real Postgres + filesystem object store**,
  build-tagged so they don't run on the default test pass but do run
  in their own CI job.
- **Migrations checksum-tracked**. A previously-applied migration
  whose checksum has changed aborts startup rather than silently re-
  applying.

---

## Privacy / compliance

- **Three-toggle HealthKit r/w with uncalibrated metadata**. Read sleep
  stages; write sessions as `inBed`; write estimated sound levels as
  `environmentalAudioExposure` with a metadata flag marking the source
  as uncalibrated.
- **Never-infer-stages-from-microphone rule**. The detector does not
  output sleep stages, period.
- **Double opt-in for audio upload**. Cloud sync ON + audio upload ON,
  both default off. Continuous all-night raw audio is never uploaded.
- **Deletion + export controls**. Per-clip delete (soft-delete +
  object delete), per-event delete, CSV/JSON streaming export, future
  account-wide delete.
- **GitHub-Pages-rendered privacy policy** (target host TBD; currently
  the disclaimer ships in `docs/DISCLAIMER.md`).

---

## Why each language

- **Swift** — native iOS, native HealthKit, native AVAudioEngine.
  SwiftUI gives us a declarative live-recording UI without bridging.
- **Rust** — deterministic detector, memory-safe ingest of raw audio
  buffers, identical crate across iOS / Mac / Linux for property +
  FFI lifecycle tests on Linux CI.
- **Go** — single static binary per service, strong stdlib for
  HTTP/auth, mature OpenTelemetry SDK, easy ops.
- **Zig** — deferred to a future audio-lab CLI (offline DSP +
  synthetic-fixture generation). Not in the iOS production path; see
  ADR 0007.

---

## What this proves

The engineer can independently scope, design, build, secure, observe,
document, and ship a multi-service mobile platform that:

- Spans three languages on the production path (Swift, Rust, Go) plus
  one tooling language reserved (Zig).
- Demonstrates security work that a backend-only or mobile-only project
  doesn't reach (refresh-token rotation, algorithm-confusion defense,
  double-opt-in raw audio, hashed telemetry user IDs).
- Demonstrates ops work (env-driven OTel, Prometheus scrape, structured
  logs, integration tests against real Postgres + blobstore).
- Demonstrates iOS-platform work (HealthKit r/w with three opt-in
  toggles, AVAudioEngine background recording, Sign in with Apple).
- Demonstrates judgement (ADRs explain *why*; ADR 0004 explicitly
  reverses a prior recommendation; ADR 0007 deliberately scopes a
  language *out* of the production path).

---

## Future roadmap

See [`ROADMAP_PRODUCTION_READY.md`](ROADMAP_PRODUCTION_READY.md) for
M0-M10+ milestones and the explicit out-of-scope list (Apple Watch
companion, Zig audio-lab CLI, SwiftData migration, distributed rate
limiting).
