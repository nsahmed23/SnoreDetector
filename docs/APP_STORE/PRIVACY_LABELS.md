# App Store Connect — Privacy Nutrition Labels

App Store Connect requires every app to fill out a privacy nutrition label answering "do you collect this data, and what for?" for each of ~14 categories. This document drafts our answers based on the current code in PRs #1–#6. Items marked **[PRODUCT DECISION]** need a human call before submission.

## Data summary

SnoreGuard processes microphone audio on-device with a heuristic detector. Audio itself never leaves the device. Some metadata leaves the device when the user opts in to backend sync (PR #3+#4) and/or HealthKit sync (PR #6 — see the type-misuse decision in that PR before shipping).

## Question 1 — Do you collect data from this app?

**Yes** — when the user signs in (Sign in with Apple) and uses the optional cross-device sync feature.

If you ship the MVP recommended in PR #6 (option (c) — drop HealthKit writes), the answer remains **Yes** because of backend sync; the HealthKit decision doesn't change this top-level answer.

## Categories

For each category Apple asks: collected? linked to user? used for tracking?

### Contact info
- **Email address**: collected if the user shares it during Sign in with Apple. Linked to user identity. **Not** used for tracking.
- Other contact fields: not collected.

### Health & fitness
- **Sleep stages**: read from HealthKit (not "collected" by Apple's definition — we don't transmit it off-device). **[PRODUCT DECISION]** confirm with legal whether reading-only into our process counts as "collected" for label purposes.
- **Other health data**: not read.

### Audio data
- **Microphone**: actively used; audio is processed on-device with the heuristic detector. **Not** stored, **not** transmitted, **not** linked to user. The user-facing audio-clip review feature in the React prototype is mock-only and is NOT in the iOS app's MVP scope.

### Usage data
- **Product interaction**: anonymously aggregated via OpenTelemetry on the backend (PR #4 + Phase 2-A). Linked to user (we have user_id_hash on every span). **Not** used for tracking — it's first-party analytics for service health, not advertising.

### Identifiers
- **User ID**: server-issued UUID, linked to Apple's `sub` (opaque user identifier). Linked to user. Not used for tracking.
- **Device ID**: not collected.
- **Apple sub**: stored on the backend as `users.apple_subject`. Linked to user. Not used for tracking.

### Diagnostics
- **Crash data**: TBD if we ship Crashlytics or Apple's built-in. **[PRODUCT DECISION]**
- **Performance data**: emitted via OpenTelemetry to our own collector. Linked to user via hashed user_id. Not used for tracking.

### Other categories (not collected)
Financial info, Location, Sensitive info, Contacts, User content (besides audio metadata), Browsing history, Search history, Purchases.

## Tracking

**App does not track users.** We do not link first-party data to third-party data, do not share data with data brokers, and do not display targeted advertising.

## Categories collected but not linked to user

None — all collected data is linked to the user account.

## Pre-submission checklist

- [ ] **[PRODUCT DECISION]** Confirm HealthKit-write decision (PR #6 options a/b/c/d) — affects whether "Health & fitness" category needs a "linked to user, transmitted" answer or just "read on-device."
- [ ] **[PRODUCT DECISION]** Confirm crash-reporting backend (Crashlytics adds a category; Apple-only doesn't).
- [ ] Legal review: privacy policy URL must be live before submission.
- [ ] Confirm the `Email` answer matches our handling: we DO store the relayed email address from Apple (`users.email`). The label must say "Yes, collected, linked to user."
- [ ] Confirm OpenTelemetry collector destination at submission time. If self-hosted: no third-party data sharing question. If we ship to a SaaS observability vendor: must disclose.

## See also

- `METADATA.md` for the human-facing description.
- `../DISCLAIMER.md` for the in-app legal copy.
