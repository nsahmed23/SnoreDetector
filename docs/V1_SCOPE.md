# SnoreGuard v1 scope

A short, opinionated answer to "what does this app actually do?".
Use this as the source of truth for what's shipping versus what's
deferred. Anything not on either of the first two lists is out of
scope for this version.

For the architecture see [`NATIVE_IOS_PORT_PLAN.md`](./NATIVE_IOS_PORT_PLAN.md).
For the legal limits on what the app may claim see
[`DISCLAIMER.md`](./DISCLAIMER.md).

---

## In v1

| Area | What ships |
|---|---|
| iOS native recording | `AVAudioEngine` capture → 16 kHz mono Float32 frames pushed into the Rust detector via FFI (`SnoreGuardCore.xcframework`, C-ABI). |
| Local persistence | Core Data store of sessions + events + clip metadata; audio clips on disk under the app sandbox. |
| HealthKit integration | Three independent opt-in toggles: read sleep stages, write inBed sessions, write uncalibrated audio-exposure samples. Each toggle is separately revocable. |
| Backend cloud sync | Sign in with Apple; bidirectional event sync; session sync; audio-clip upload (multipart, server-side sha256 verification); CSV + JSON export. |
| Backend hardening | Per-IP and per-user rate limiting on auth + ingest endpoints; per-user export budget (rows / day); structured logs without PII. |
| Observability | OpenTelemetry traces + metrics across sync, analytics, and export services; OTLP exporter env-driven, no-op when unset. |
| Privacy + legal copy | In-app `DISCLAIMER.md`, hosted `PRIVACY_POLICY.md`, App-Store nutrition labels, account-deletion path documented. |

## Deferred to v1.1+

| Area | Why deferred |
|---|---|
| Apple Watch companion | Watch capture introduces a different audio session model and a separate App-Store target; not on the v1 critical path. |
| Zig-based offline audio-bench CLI | Useful for tuning the detector against a labeled corpus, but not on the user-visible path. |
| SwiftData migration | Core Data is fine for v1; SwiftData's iOS-17-only baseline rules out users we want. |
| Distributed rate limiting (Redis) | The single-instance in-memory limiter holds for v1 traffic; revisit when we scale beyond one sync-service replica. |
| `HKObserverQuery` for background HealthKit | v1 reads sleep on demand at sync time. Background observers add complexity without changing the data we write. |
| Signed-URL clip downloads | v1 reads clips from the local store; web-side history view that needs server-issued download URLs is post-v1. |
| Persistent retry queue | The in-process queue is good enough for one device + intermittent connectivity. A durable on-disk queue waits for the multi-device story. |
| ML-based detector | The heuristic in `rust-core/snoreguard-core` is the v1 detector. ML retraining pipeline + on-device inference is a v2 effort. |

## Out of scope forever

These will not be added to SnoreGuard at any version. They would
either misrepresent what the app does or invite the FDA / CE-marking
regime that this project explicitly declines to enter.

| Item | Why not |
|---|---|
| Medical claims of any kind | The app is a wellness prototype, not a medical device. See `DISCLAIMER.md`. |
| Sleep-apnea screening or scoring | Requires polysomnography validation and FDA clearance (US) / CE marking under MDR (EU). Not happening. |
| REM / core / deep stage inference from microphone | The microphone signal has insufficient information to infer sleep stages. Apple Health stage data we *read* comes from the watch sensors, not us. |
| Calibrated sound-pressure-level meter readings | Requires a reference-grade microphone. Phone microphones vary by model and case, and we don't ship a calibration step. The "uncalibrated" qualifier on the HealthKit audio-exposure write is intentional and load-bearing. |
