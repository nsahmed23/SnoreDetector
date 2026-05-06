# SnoreGuard — roadmap to production

A milestone-by-milestone plan from the current state (planning + scaffold
phase) to a public App Store release plus a recruiter-ready portfolio
case study. Each milestone is a coherent, independently reviewable
chunk of work; later milestones depend on the earlier ones being
landed.

For background, see [`ARCHITECTURE.md`](ARCHITECTURE.md), the
[ADRs](ADRs/), [`PORTFOLIO_NARRATIVE.md`](PORTFOLIO_NARRATIVE.md), and
[`DEPLOYMENT.md`](DEPLOYMENT.md).

---

## M0 — Repo hygiene

**Status: shipped.**

- Web prototype cleaned up; honest copy on the disclaimer.
- Planning docs (`PRODUCT_SPEC.md`, `NATIVE_IOS_PORT_PLAN.md`,
  `DISCLAIMER.md`) merged on `main`.
- CI workflows green for Rust core, Go backend (unit + integration),
  iOS scaffold, and the React prototype.
- Backend already includes Apple Sign-In, refresh tokens with rotation,
  compound-cursor pagination, rate limiting, per-user export budget,
  and OpenTelemetry scaffolding.

---

## M1 — Mac build + iPhone run

First time the native app actually runs on real hardware.

- `rust-core/scripts/build-xcframework.sh` produces a clean
  `SnoreGuardCore.xcframework` on a Mac.
- Xcode project compiles against the xcframework; `SnoreCore.swift` is
  the only Swift type that touches raw C pointers.
- App launches in the iOS simulator with the three-tab `ContentView`
  rendering.
- App launches on a wired iPhone via `xcodebuild run` after signing
  with the team's standard provisioning profile.
- Microphone permission prompt fires; the Record tab shows a live
  volume meter driven by real audio.

Exit criteria: a video walkthrough of the app running on an iPhone with
the live meter responding to speech and ambient noise.

---

## M2 — HealthKit read/write

The three-toggle HealthKit surface (per ADR 0004).

- `HealthStore` Swift protocol + real `HKHealthStore`-backed
  implementation.
- Settings UI exposes three toggles, each driving its own
  `requestAuthorization` call. The toggle state reflects real
  `HKHealthStore.authorizationStatus(for:)`.
- Read sleep stages → overlay on the Insights tab timeline.
- Write each recording session as `HKCategorySample(.inBed)`.
- Write each session's average sound level as
  `HKQuantitySample(environmentalAudioExposure)` with metadata flag
  `SnoreGuard.Source = "uncalibrated_microphone"`.
- Fake-client tests assert the three-toggle independence and the
  uncalibrated metadata key.

Exit criteria: end a recording session with all three toggles on, open
Apple Health, see the `inBed` block and the audio-exposure sample with
the uncalibrated metadata key visible.

---

## M3 — Local history + audio clips

Local persistence and on-device clip storage.

- `EventStore` (Core Data) persists sessions and events.
- `AudioEngine` maintains a rolling buffer; on each finalized event,
  extracts a clip of `clip_duration` seconds around the event and
  writes it to the app container.
- Insights tab lists past sessions with a per-session clip review.
- Settings exposes `clip_duration` and "Keep local clips for…"
  retention.

Exit criteria: record a session, kill the app, relaunch, see the past
session and play back its clips.

---

## M4 — Backend cloud sync (sessions + events)

The first network-touching milestone.

- `AuthManager` runs the Sign-in-with-Apple flow and exchanges the
  Apple identity token for SnoreGuard access + refresh tokens via
  `POST /auth/apple`. Tokens stored in Keychain
  (`AfterFirstUnlockThisDeviceOnly`).
- `APIClient` retries on 401 by calling `POST /auth/refresh`; if that
  also fails, surfaces a sign-in prompt.
- `SyncManager` pushes new sessions and events to the backend; uses
  cursor pagination to pull on a second device.
- Settings exposes the cloud-sync toggle.

Exit criteria: install the app on two devices with the same Apple
account, record on one, see the events on the other after a sync pass.

---

## M5 — Raw audio upload + storage

The double-opt-in raw-audio path.

- New backend endpoints: `POST /audio/clips` (multipart with sha256),
  `GET /audio/clips/{id}/download`, `DELETE /audio/clips/{id}`
  (soft-delete + object delete), `GET /audio/clips?cursor=...`.
- `internal/blobstore` interface + `fs` + `fake` implementations land;
  GCS / S3 implementations are stubbed for ADR 0008.
- Per-clip retention sweeper job (configurable cap, default 30 d).
- iOS Settings exposes the audio-upload toggle (visible only when
  cloud sync is on) and the cloud retention setting.

Exit criteria: with both toggles on, recording a session uploads its
clips; second device can download them; per-clip delete from either
device propagates.

---

## M6 — Telemetry dashboards + alerting

Production-grade observability per `OBSERVABILITY.md`.

- Production OTel collector deployed and receiving from all three
  services.
- Grafana dashboards built for: error-rate per route, p50/p95/p99
  handler latency, auth-success ratio, refresh-theft alarms, export
  budget exhaustion, blobstore failure rate, audio-clip volume.
- Alerts wired for: 5xx rate spike, invalid-token spike, refresh-theft
  events, blobstore failures, DB unreachable, OTel collector down.
- SLO targets recorded and tracked.

Exit criteria: a refresh-theft test event in staging triggers the
alert; dashboard panels populate from real traffic.

---

## M7 — TestFlight

Internal-only beta.

- App signed with the team distribution profile; uploaded to App Store
  Connect.
- Internal testers (2-5 people) added.
- TestFlight smoke test (per `TEST_STRATEGY.md`) passes for every build
  promoted to internal testing.
- Crash reporter wired (host TBD — open product decision).

Exit criteria: internal testers can install via TestFlight, run a
night, and the next morning see the data on a second device + in
Apple Health.

---

## M8 — App Store

Public release.

- App Store metadata: privacy nutrition labels reflect the data
  classes documented in `PRIVACY_AND_DATA_LIFECYCLE.md`.
- Privacy policy URL is hosted (host TBD).
- Submission package: screenshots, description, support URL, marketing
  URL.
- App Review correspondence captured for the case study.

Exit criteria: app available on the App Store under the team account;
download + sign-in works on a fresh device.

---

## M9 — Recruiter portfolio polish

Convert the engineering work into a portfolio asset.

- Case-study writeup linking each milestone to the corresponding ADR
  and PR.
- Screenshots and a short video walkthrough.
- Cleaned-up README + landing page emphasizing the architecture and
  decisions, not just the feature list.
- `PORTFOLIO_NARRATIVE.md` updated against the shipped reality.

Exit criteria: a recruiter who reads only the case study can
understand what the engineer built and why each architectural choice
was made.

---

## M10+ — Out of scope (for now)

Documented here so they don't sneak into earlier milestones:

- **Apple Watch companion**. Sleep tracking from the wrist would be
  valuable but is a multi-week project on its own.
- **Zig audio-lab CLI**. Lives only in a future
  `claude/zig-audio-lab` branch; offline DSP + synthetic-fixture
  generation; not in the iOS production path. Per ADR 0007.
- **SwiftData migration**. Core Data is fine for v1; SwiftData is a
  future cleanup.
- **Distributed rate limiting**. Today the rate limiter is single-
  replica in-memory. Multi-replica deploys need either a Redis-backed
  limiter or edge-layer enforcement.
- **Persistent retry queue on iOS**. Sync retries are in-memory today;
  killing the app loses a not-yet-retried upload.
- **Reconciliation sweeper for orphaned object-store keys**.
- **Self-service "Delete my account"** UI. Today this is a support
  contact path.
- **Signed download URLs** for clips. Today downloads are proxied; a
  future optimization for high-traffic clip serving.
