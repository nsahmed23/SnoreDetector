# SnoreGuard test strategy

The SnoreGuard codebase spans Swift (iOS app), Rust (detector core), and
Go (three backend services). This document describes the test pyramid
across all three, what runs in CI, and what requires Mac or device
hardware.

---

## Swift unit tests

Run via `xcodebuild test -scheme SnoreGuard -destination
'platform=iOS Simulator,name=iPhone 15'` on a Mac. Targeted at logic
classes that don't need real audio hardware:

- **SnoreCore FFI shape**. Verifies that `SnoreCore` correctly imports
  the C ABI from `SnoreGuardCore.xcframework`:
  - `pushFrame` accepts a 256-sample buffer.
  - `pollEvent` returns nil when no event, populated `SgEvent` when
    one is ready.
  - `deinit` calls `sg_detector_free` exactly once.
  - Status-code mapping into `SnoreCoreError`.
- **DetectorSettings snap behaviour**. Threshold + sensitivity changes
  persist in `UserDefaults` and propagate into the next
  `SnoreCore.set*` call.
- **HealthRecorder fakes**. A `HealthStore` protocol with a fake
  conforming implementation; tests verify the three-toggle flow:
  authorize → write `inBed` → write `environmentalAudioExposure` with
  the uncalibrated metadata key set.
- **APIClient URLProtocol mock**. Tests use a `URLProtocol` subclass to
  stub HTTP responses; cover `/auth/apple`, `/auth/refresh`, `/events`,
  `/audio/clips`, and the 401-then-refresh flow.

---

## Swift integration tests

Mac-only; require AVFoundation but not a physical device:

- **AudioEngine bootstrap**. Configures `AVAudioSession`, taps
  `inputNode` with a synthetic `AVAudioPCMBuffer`, asserts frames flow
  end-to-end into a stub detector.
- **EventStore Core Data round-trip**. Insert an event, fetch by
  session id, delete, assert empty.

---

## Rust unit + property + FFI integration

Run on Linux CI on every push. No Mac required.

- **Unit tests** in `rust-core/snoreguard-core/src/detector.rs` cover:
  - FFT magnitude → uncalibrated dB-ish output range stays in `[30,
    100]`.
  - Low-frequency dominance ratio thresholds (Low/Medium/High).
  - Sustained-frame counter triggers an event after exactly
    `DEFAULT_SUSTAIN_FRAMES` frames.
- **Property tests** (via `proptest`) generate arbitrary 256-sample
  buffers and assert:
  - The detector never panics.
  - Event timestamps are monotonically increasing.
  - `pushFrame` allocates zero per call (asserted via a custom
    allocator wrapper in the test build).
- **FFI lifecycle integration tests** in
  `rust-core/snoreguard-core/tests/integration.rs`:
  - `sg_detector_new` / `sg_detector_free` round-trip.
  - NULL safety of `sg_detector_free`.
  - Invalid-argument handling (NaN threshold, sample_rate_hz == 0).
  - End-to-end push → poll across many frames.

`cargo test` is gating; `cargo clippy -- -D warnings` is gating.

---

## Go unit tests

Run on every push; no DB, no network. Per-package handler tests with
fakes:

- `internal/apple` — Apple identity-token verifier with a fake JWKS.
- `internal/jwt` — HS256 issue/verify; algorithm-confusion rejection;
  expired-token rejection.
- `internal/auth` — middleware tests with a fake `revoked_jti` lookup.
- `internal/server` — `/auth/apple`, `/auth/refresh`, `/auth/logout`,
  `/events` (POST + GET) handler tests with an in-memory store.
- `internal/analytics` — `/analytics/summary`, `/analytics/totals` with
  an in-memory store.
- `internal/export` — `/export/events.csv`, `/export/events.json`
  streaming + cross-user filter tests with an in-memory store.
- `internal/blobstore/fake` — interface compliance + sha256 mismatch
  rejection.

Run via `make test`.

---

## Go integration tests

Build-tagged `-tags=integration`. Require a real Postgres reachable via
`DATABASE_URL` and a real filesystem blobstore directory.

- Migrations apply cleanly from empty schema.
- Re-applied migrations are no-ops; checksum mismatch aborts.
- User upsert idempotence.
- Event insert idempotence on `client_event_id`.
- Compound-cursor pagination covers same-`received_at` batches without
  truncation.
- Refresh-token rotation + family revocation on reuse.
- Filesystem blobstore put → get → delete round-trip with sha256.

Run via `make test-integration`. Gated behind `//go:build integration`
so they never run on the default test pass.

---

## API contract tests

Schema-validate the live HTTP responses against the JSON examples in
`backend/README.md`. Implementation: a small Go test that boots the
sync service against the integration Postgres + fs blobstore, drives
each endpoint with `httptest`, and asserts the response shape matches a
JSON schema derived from the README examples.

Run alongside the integration suite.

---

## HealthKit fake-client tests

Phase C lands a `HealthStore` protocol with a fake conforming
implementation. The tests assert:

- The three opt-in toggles drive three independent
  `requestAuthorization` calls.
- Sound-level samples carry the
  `SnoreGuard.Source = "uncalibrated_microphone"` metadata key.
- The "never infer stages from microphone" rule: there is no code path
  that writes a sleep stage as a function of detector output.

These are Swift tests; they do not require a real
`HKHealthStore` because the protocol indirection lets the fake stand
in.

---

## Object-store fake tests

`internal/blobstore/fake` is an in-memory implementation of
`blobstore.Store`. Compliance tests assert every implementation
(`fake`, `fs`, future `gcs`, future `s3`) satisfies the same
behavioural contract:

- `Put` rejects sha256 mismatch.
- `Put` refuses to overwrite an existing key.
- `Get` returns a `not found` error for an unknown key.
- `Delete` is idempotent.
- `Stat` returns metadata or `not found`.

A single test table runs against each implementation in CI.

---

## End-to-end local Docker tests

Driven by a shell script under `backend/scripts/e2e.sh`:

```bash
docker compose --profile app up -d
# wait for /healthz on :8080, :8081, :8082
# curl /auth/apple with a fixture token, capture access + refresh
# curl POST /events with a small batch, assert 200 + inserted count
# curl GET /events?cursor= and reconstruct the full set
# curl POST /audio/clips with a small wav and a sha256, assert 201
# curl GET /audio/clips/{id}/download, assert byte equality
# curl GET /export/events.csv, assert non-empty
# curl DELETE /audio/clips/{id}, assert 204
docker compose down -v
```

Runs nightly in CI; runnable locally for debugging.

---

## First Mac / iPhone verification tests

Manual smoke tests covered by the future `docs/MAC_QUICKSTART.md`
(open work for M1):

- `xcodebuild build` succeeds on a clean checkout.
- The app launches in the simulator.
- The app launches on a wired iPhone after `xcodebuild run`.
- The Record tab gets microphone permission and a live volume meter
  appears.
- A clap or low rumble produces a visible event in the meter.
- The Insights tab shows the event after stopping the recording.
- Settings reflects HealthKit authorization status correctly.

---

## TestFlight smoke test

Pre-release checklist before promoting a TestFlight build to App Store:

- Lock-screen recording: start a session, lock the device, verify the
  background-audio entitlement keeps the engine alive (the iOS
  recording indicator stays on the Dynamic Island / status bar).
- HealthKit write round-trip: enable the three toggles, end a session,
  open Apple Health → Browse → Audio Exposure, see the
  `environmentalAudioExposure` sample with the uncalibrated metadata
  flag.
- Cloud sync round-trip: enable cloud sync, end a session, install
  the build on a second device with the same Apple account, verify
  the session + events appear via cursor pagination.
- Audio upload round-trip: enable both toggles, end a session,
  verify the clip uploads (network logs show multipart POST), open
  the second device, verify the clip downloads on demand.
- Export download: open the export sheet, request CSV, verify the
  file lands in Files via the share sheet.

These are manual; they gate every public TestFlight release.
