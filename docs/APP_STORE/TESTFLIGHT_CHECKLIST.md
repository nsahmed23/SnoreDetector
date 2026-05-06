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

## 5. HealthKit (medium friction — three independent toggles)

- [ ] Add the entitlement to the App ID with both read and write scopes.
- [ ] Confirm the disclaimer in `DISCLAIMER.md` matches the actual write/read behavior.

## 5a. HealthKit r/w smoke test
- [ ] Settings → enable "Read sleep stages" — system permission sheet appears with read-only request
- [ ] Settings → enable "Write SnoreGuard sessions" — system permission sheet appears with write-session-only request
- [ ] Settings → enable "Write estimated sound levels" — write-audio-exposure permission requested
- [ ] Run a 1-min recording with all three toggles on; confirm:
  - sessionWrites lands in Health.app under Sleep → "SnoreGuard"
  - per-event sound-level samples land under Hearing → Environmental Sound Levels
  - sleep stages from Health are visible in History (correlation badges)
- [ ] Toggle off "Write sessions" → record again → no new sleep-analysis sample

## 6. Backend smoke test (low friction if local; medium if cloud)

- [ ] `cd backend && docker compose up -d` brings Postgres + the three services.
- [ ] From the iPhone (on the same Wi-Fi as the dev Mac), point `APIClient.base = "http://<mac-IP>:8080"`.
- [ ] Sign in via the Sign in with Apple button — confirm `/auth/apple` returns access + refresh tokens.
- [ ] Push a synthetic event, list it back via `/events`.

## 6a. Cloud sync smoke test
- [ ] Settings → toggle "Cloud sync" on — Sign in with Apple sheet appears
- [ ] Sign in; backend (`make run-sync`) logs an /auth/apple POST with 200 + token pair
- [ ] Record a 30-second session; stop
- [ ] Backend logs show POST /sessions and POST /events with 200
- [ ] Reinstall app, sign in again, History tab shows the previously-recorded session

## 6b. Audio clip upload smoke test
- [ ] Settings → enable "Upload audio clips" (cloud sync must already be on)
- [ ] Record a session that includes a real snore-like sound; stop
- [ ] Backend logs show POST /audio/clips with 200; sha256 verification passed
- [ ] History → tap session → tap a clip → playback works (or "v1.1" placeholder if playback wasn't included)
- [ ] Health-app cross-check: per-event sound-level samples appear at the matching timestamps

## 6c. Export-with-clips smoke test
- [ ] curl with bearer token: `curl 'http://<mac-ip>:8082/export/events.json?include=sessions,clips' -H 'Authorization: Bearer $TOKEN' | jq .`
- [ ] Response has events, sessions, and clips arrays plus a count object
- [ ] Confirm clip metadata includes object_key (UUID-derived, never a user-provided string)
- [ ] Optionally download a clip via `GET /audio/clips/{id}/download`; bytes match the local file

## 6d. Deletion + privacy controls smoke test
- [ ] History → delete a clip → confirm:
  - local file at `app-support/audio/<id>.m4a` is removed
  - backend POST shows DELETE /audio/clips/{id} with 200
  - subsequent GET /audio/clips/{id}/download returns 404
- [ ] Settings → log out → confirm tokens removed from Keychain (no further sync attempts)

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

- **Privacy policy URL** — needs to be live. Recommendation: a GitHub Pages render of `../PRIVACY_AND_DATA_LIFECYCLE.md` (Phase A doc).
- **`@google/genai` keep or drop** in `package.json` (PR #1 follow-up issue) — does not affect iOS submission directly but matters for the public web prototype.

The HealthKit r/w decision (full r/w with three opt-in toggles) and the crash-reporting decision (Apple-only for v1) are resolved — see `PRIVACY_LABELS.md`.

## See also

- [../RELEASE_CHECKLIST.md](../RELEASE_CHECKLIST.md) — full release checklist (Phase H sibling doc).
- [../MAC_QUICKSTART.md](../MAC_QUICKSTART.md) — first-Mac-session quickstart (Phase H sibling doc).
