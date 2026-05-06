# App Store Connect — Privacy Nutrition Labels

App Store Connect requires every app to fill out a privacy nutrition label answering "do you collect this data, and what for?" for each of ~14 categories. This document drafts our answers based on the v1 scope (heuristic detector, Sign in with Apple + cross-device sync, three-toggle HealthKit, opt-in audio-clip upload). Items marked **[PRODUCT DECISION]** still need a human call before submission.

## Data summary

SnoreGuard processes microphone audio on-device with a heuristic detector. By default, audio never leaves the device. Some metadata leaves the device when the user opts in to backend sync. Short audio clips around detected events leave the device only when the user opts in to BOTH cloud sync AND the audio-clip-upload toggle. HealthKit data is read and/or written on-device when the user enables the corresponding HealthKit toggles; SnoreGuard itself never transmits HealthKit data off-device.

## Question 1 — Do you collect data from this app?

**Yes** — when the user signs in (Sign in with Apple) and uses the optional cross-device sync feature. The HealthKit toggles do not change this top-level answer; HealthKit data is read/written on-device only and is not "collected" under Apple's privacy framework.

## Categories

For each category Apple asks: collected? linked to user? used for tracking?

### Contact info
- **Email address**: collected if the user shares it during Sign in with Apple. Linked to user identity. **Not** used for tracking.
- Other contact fields: not collected.

### Health & fitness — Yes, collected, linked to user, not used for tracking.

When the user enables the "Write SnoreGuard sessions to Apple Health" toggle, sessions are written to the user's own Apple Health record as `sleepAnalysis` category samples with value `inBed`. When the user enables "Write estimated sound levels to Apple Health", per-event `environmentalAudioExposure` samples are written with metadata flagging them as estimated/uncalibrated/not-diagnostic. Apple Health data is **never** transmitted off the user's device by SnoreGuard — Apple's own iCloud sync is what propagates Health data, governed by the user's iCloud settings. Reading sleep stages (when the read toggle is on) is processed on-device only and never uploaded to SnoreGuard's backend.

**HealthKit reads are not "collected" for label purposes** — Apple's privacy framework distinguishes "collected" (data leaves the user's device or is shared with third parties) from "accessed on-device". SnoreGuard reads sleep-stage samples on-device only; nothing about that read travels to our backend or any third party. Therefore: read access is declared in the entitlement file but does NOT need a privacy-label entry under "Health & fitness — Linked to you".

### Audio data — Yes, collected (when user opts in to cloud sync of clips), linked to user, not used for tracking.

By default, microphone audio is processed on-device for snore-like-event detection only and never recorded or uploaded. When the user enables BOTH the "Cloud sync" toggle AND the "Upload audio clips" toggle in Settings, short audio clips around detected events are uploaded to the user's account on SnoreGuard's backend. Clips are not used for tracking, are deletable per-clip from the History tab (which removes them from both device and backend), and are export-available via the CSV/JSON export endpoints. Audio is never used for advertising, never sold, and never shared with third parties.

### Usage data
- **Product interaction**: anonymously aggregated via OpenTelemetry on the backend (PR #4 + Phase 2-A). Linked to user (we have user_id_hash on every span). **Not** used for tracking — it's first-party analytics for service health, not advertising.

### Identifiers
- **User ID**: server-issued UUID, linked to Apple's `sub` (opaque user identifier). Linked to user. Not used for tracking.
- **Device ID**: not collected.
- **Apple sub**: stored on the backend as `users.apple_subject`. Linked to user. Not used for tracking.

### Diagnostics
- **Crash data — Apple's built-in crash reporting only.** v1 does not ship a third-party crash SDK (Crashlytics, Sentry, etc). Apple's anonymous crash reports are submitted only when the user has consented in iOS Settings → Privacy → Analytics & Improvements → Share with App Developers.
- **Performance data**: emitted via OpenTelemetry to our own collector. Linked to user via hashed user_id. Not used for tracking.

### Other categories (not collected)
Financial info, Location, Sensitive info, Contacts, User content (besides audio clips disclosed above), Browsing history, Search history, Purchases.

## Tracking

**App does not track users.** We do not link first-party data to third-party data, do not share data with data brokers, and do not display targeted advertising.

## Categories collected but not linked to user

None — all collected data is linked to the user account.

## Pre-submission checklist

- [x] HealthKit-write status: full r/w ships in v1, with three independent opt-in toggles. Data never leaves the device via SnoreGuard (Apple's iCloud handles propagation, governed by user's iCloud settings). No "transmitted" label entry required.
- [x] HealthKit-read classification: on-device read, not "collected" under Apple's privacy framework. Declared in the entitlement file; no privacy-label entry under "Health & fitness — Linked to you".
- [x] Crash-reporting backend: Apple-only for v1. No third-party crash SDK.
- [ ] **[PRODUCT DECISION]** Privacy policy URL — still pending production deployment. **Recommendation:** GitHub Pages render of `../PRIVACY_AND_DATA_LIFECYCLE.md` (the Phase A doc) — that file is the canonical narrative for what's collected, where it goes, and how long it lives.
- [ ] Confirm the `Email` answer matches our handling: we DO store the relayed email address from Apple (`users.email`). The label must say "Yes, collected, linked to user."
- [ ] Confirm OpenTelemetry collector destination at submission time. If self-hosted: no third-party data sharing question. If we ship to a SaaS observability vendor: must disclose.

## See also

- [../PRIVACY_AND_DATA_LIFECYCLE.md](../PRIVACY_AND_DATA_LIFECYCLE.md) — canonical narrative for what data is collected, where it lives, and retention/deletion lifecycle.
- [../THREAT_MODEL.md](../THREAT_MODEL.md) — system-level threat model and mitigations.
- `METADATA.md` for the human-facing description.
- `../DISCLAIMER.md` for the in-app legal copy.
