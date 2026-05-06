# ADR 0004: HealthKit read/write in v1 (three opt-in toggles)

## Status

Accepted. **This reverses an earlier "drop writes for MVP (option c)"
recommendation.**

## Context

HealthKit integration is the highest-trust surface in the iOS app —
writing to a user's Apple Health record places data inside the same
store that medical providers, regulated apps, and other wellness apps
read from. A previous internal recommendation suggested deferring all
HealthKit writes for the MVP and shipping read-only first, on the
grounds that:

- Apple App Review scrutinizes HealthKit usage strings and write scopes.
- Sound-level metrics are uncalibrated; we did not want users or
  third-party apps treating them as reliable measurements.
- Read-only is simpler.

In the meantime the product scope expanded to "portfolio-grade
end-to-end mobile platform". For that target, shipping read-only is no
longer enough — the v1 surface must demonstrate full HealthKit
integration including writes, with the trust caveats addressed
explicitly rather than avoided.

## Decision

Ship full HealthKit read/write in v1 behind **three independent opt-in
toggles** in Settings, each driving a separate
`HKHealthStore.requestAuthorization` call. The toggles:

1. **Read sleep stages** — `HKCategoryType(.sleepAnalysis)`. Used only
   to overlay sleep-stage shading in the Insights tab. **Never used to
   infer REM / core / deep sleep stages from the microphone signal.**
   That inference would be a quasi-medical claim and is explicitly out
   of scope.
2. **Write sessions as `inBed`** — writes each recording session as an
   `HKCategorySample(.inBed)` for the session's start/end times.
3. **Write estimated sound levels as `environmentalAudioExposure`** —
   writes the session's average uncalibrated sound level as an
   `HKQuantitySample` with metadata flags marking the source as
   uncalibrated and the device as a consumer-grade microphone (key:
   `SnoreGuard.Source = "uncalibrated_microphone"`).

The Settings UI reflects the real
`HKHealthStore.authorizationStatus(for:)` per toggle, not just a local
boolean, so revoking access from iOS Settings → Health is reflected
immediately.

## Consequences

**Positive**:

- Demonstrates end-to-end HealthKit r/w — the core differentiator over a
  read-only or fully app-local approach.
- Granular consent: a user can opt in to reads only, or write sessions
  but not sound levels, etc.
- Uncalibrated metadata flag means downstream consumers know the data is
  not measurement-grade, which is the honest framing.
- The "never infer stages from microphone" rule is a clear
  product-level constraint that bounds what kind of insights we are
  allowed to surface.

**Negative**:

- App Review surface is larger; we need three usage strings in
  Info.plist that survive review.
- Three toggles is more UI than one master switch; users need to
  understand the difference. Mitigated by a "How HealthKit is used"
  link in Settings.
- We must keep the metadata flag and the never-infer rule honoured in
  every code path. This is enforced by code review and by tests in
  `HealthRecorderTests` (Phase C).

**Neutral**:

- We do not write `HKWorkout` for sleep sessions in v1. `inBed` plus a
  custom sound-level sample is the smallest correct surface; whether to
  also wrap the session in an `HKWorkout` is deferred.

## Alternatives considered

- **Read-only forever** (the original "option c" recommendation).
  Rejected: makes the integration a glorified visualizer. The whole
  point of HealthKit is the bidirectional store; demonstrating only one
  direction undersells the product and removes the most interesting
  trust-engineering surface.
- **App-local only, no HealthKit at all**. Rejected: the iOS user's
  expectation for a sleep / snoring app is HealthKit integration. An
  app that doesn't talk to Apple Health feels half-built. The
  uncalibrated-sound-level concern is real but it is a labeling problem,
  not an integration problem.
