# TestFlight checklist (first Mac session)

Use this as the running checklist when you sit down at a Mac with Xcode 15+ for the first time after Phases 0–4 land. Items grouped by likely friction.

## 0. Prereqs

- [ ] Xcode 15.4 or later installed.
- [ ] An Apple Developer Program membership in good standing.
- [ ] The `com.snoreguard.app` Bundle ID created in the Apple Developer portal with capabilities: Sign in with Apple, HealthKit, Background Modes (audio).
- [ ] An App Store Connect app record created with the bundle ID.

## 1. Build the Rust xcframework (high friction expected)

- [ ] `cd rust-core` and `rustup target add aarch64-apple-ios aarch64-apple-ios-sim x86_64-apple-ios`.
- [ ] Run `./scripts/build-xcframework.sh`. Likely friction: `SDKROOT` not set, missing `xcrun`, simulator-vs-device slice mismatch.
- [ ] Confirm `rust-core/SnoreGuardCore.xcframework` exists with three slices.

## 2. Generate the Xcode project (medium friction)

- [ ] `brew install xcodegen` (one-time).
- [ ] `cd ios && xcodegen generate`.
- [ ] Open `SnoreGuard.xcodeproj` in Xcode.

## 3. First simulator build (medium friction — the blind-authored Swift surface meets the compiler)

- [ ] Select an iPhone 15 simulator and Cmd+B.
- [ ] Likely fix-needed items (already flagged in PR #5's least-confident list):
  - `AVAudioApplication.shared.recordPermission` API spelling. iOS 17 introduced this; older spelling is `AVAudioSession.requestRecordPermission`.
  - `AVAudioPCMBuffer.floatChannelData` indexing on the non-interleaved mono format.
- [ ] Run on simulator: consent sheet appears, then Record tab.

## 4. First device build (low friction once simulator passes)

- [ ] Provisioning profile auto-resolves with Xcode-managed signing.
- [ ] Microphone permission prompt fires on first "Start listening."
- [ ] Audio path verified: tap on a real microphone, see the level meter respond.
- [ ] Lock-screen test: start recording, lock the device, observe detection continues (the `audio` background mode is enabled).

## 5. HealthKit (medium friction — depends on PR #6 decision)

- [ ] Confirm option (a/b/c/d) from PR #6's "Decisions needed" section.
- [ ] If (a): add the entitlement to the App ID, smoke test the toggle flow, verify a sample lands in Health.app.
- [ ] If (c — recommended): drop the write capability from the entitlement, keep the read scope, smoke test the toggle.
- [ ] Either way: confirm the disclaimer in `DISCLAIMER.md` matches the actual write/read behavior.

## 6. Backend smoke test (low friction if local; medium if cloud)

- [ ] `cd backend && docker compose up -d` brings Postgres + the three services.
- [ ] From the iPhone (on the same Wi-Fi as the dev Mac), point `APIClient.base = "http://<mac-IP>:8080"`.
- [ ] Sign in via the Sign in with Apple button — confirm `/auth/apple` returns access + refresh tokens.
- [ ] Push a synthetic event, list it back via `/events`.

## 7. App Store Connect submission (high friction first time)

- [ ] Set version to 0.1.0 in Xcode and App Store Connect.
- [ ] Upload archive: Product → Archive → Distribute App → App Store Connect.
- [ ] Fill in privacy nutrition labels (see `PRIVACY_LABELS.md`).
- [ ] Fill in metadata (see `METADATA.md`).
- [ ] Upload screenshots (see `SCREENSHOTS_PLAN.md`).
- [ ] Submit for review.

## 8. TestFlight invite

- [ ] Add internal testers in App Store Connect.
- [ ] First invite email + sample run-through.

## Open decisions blocking submission

These must be resolved before clicking "Submit for review":

- **HealthKit type-misuse decision** (PR #6 options a/b/c/d).
- **`@google/genai` keep or drop** in `package.json` (PR #1 follow-up issue) — does not affect iOS submission directly but matters for the public web prototype.
- **Privacy policy URL** — needs to be live (suggestion: a GitHub Pages render of `DISCLAIMER.md`).
- **Crash reporting backend** — Crashlytics vs Apple-only.
