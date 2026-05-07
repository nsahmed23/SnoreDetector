# First Mac Session Runbook

The single linear playbook for the first time SnoreGuard runs on real
Apple hardware. Assumes a fresh Mac, no prior setup, and that all
pre-Mac PRs have merged into `main`.

This runbook does **not** claim Mac verification has happened — it is
the script the operator follows to perform that verification.
Until every step here passes, treat the Mac path as untested.

For the broader architectural picture see
[`NATIVE_IOS_PORT_PLAN.md`](./NATIVE_IOS_PORT_PLAN.md). For the
disclaimer the app must show, see [`DISCLAIMER.md`](./DISCLAIMER.md).
For the cloud deployment topology see [`DEPLOYMENT.md`](./DEPLOYMENT.md).

---

## Locked decisions (pre-Mac freeze)

These are the v1 product/infra defaults. They are recorded here so the
runbook is self-contained and so reviewers can challenge a single
document instead of hunting through threads. Anything not on this list
is out of scope until the first Mac session has shipped a build.

| Decision | Value |
|---|---|
| App name | **SnoreGuard** (engineering + product; verify trademark/App Store availability before public launch) |
| Bundle ID | `com.nsahmed.snoreguard` |
| GCP project | `snoreguard-v1` |
| Primary GCP region | `us-central1` |
| GCS bucket | `snoreguard-clips-prod` (fallback `snoreguard-clips-v1-prod` if taken) |
| GCS bucket region | `us-central1` (matches Cloud Run + Cloud SQL) |
| Cloud audio retention | **30-day default**, user can delete earlier; longer retention is a future explicit user setting |
| Privacy policy host | GitHub Pages |
| Support URL host | GitHub Pages (with mailto contact) |
| Cloud download strategy | Stream through API in v1 — no signed URLs |
| Signed URLs | Deferred to v1.x |
| HealthKit | Read sleep + write inBed sessions + write uncalibrated audio-exposure — all opt-in |
| Backend cloud sync | In v1, behind explicit toggle |
| Raw audio upload | In v1, double opt-in only |
| App Store posture | Wellness / non-diagnostic; HealthKit r/w; raw audio with explicit consent |

> **Retention wording note.** The "30 days" is a *default*, not a
> floor. The app must allow the user to delete clips earlier than 30
> days. A floor would be a privacy regression and is not the intent.

Forbidden claims are regex-enforced in CI. The full pattern lives in
[`PRIVACY_AND_DATA_LIFECYCLE.md`](./PRIVACY_AND_DATA_LIFECYCLE.md) and
the `scripts/dev/check-claims.sh` script — do not duplicate the
literal pattern here, because grepping for "the forbidden words" in
this doc would itself trip the check.

---

## Pre-flight checklist (do these before sitting down at the Mac)

- [ ] Apple Developer Program enrollment is **active** (paid, not pending).
- [ ] GCP project `snoreguard-v1` exists; billing attached.
- [ ] GCS bucket `snoreguard-clips-prod` (or fallback) is created in
      `us-central1` with uniform-IAM and versioning on.
- [ ] Cloud SQL Postgres instance exists in `us-central1` with PITR on,
      *or* you accept smoke-testing against local Docker Postgres only
      on day one.
- [ ] Privacy policy and support pages are published on GitHub Pages
      (placeholder URLs are acceptable for TestFlight; replace before
      App Store submit).
- [ ] You know your Apple Team ID (`https://developer.apple.com/account`
      → Membership).

If any item is missing, stop and finish it before continuing — every
Mac step downstream assumes these are done.

---

## The 17 steps

Each step lists: **what runs**, **expected output** (or success signal),
**what to do if it fails**. Run them top-to-bottom. Do not skip ahead.

### 1. Install Xcode

App Store → search "Xcode" → install (≥ 15.4). First launch accepts the
license.

```sh
xcodebuild -version
```

Expected:
```
Xcode 15.4
Build version 15F31d
```

If `xcodebuild` is missing or reports a version older than 15.4: update
via the App Store before continuing. Older Xcode versions miss APIs the
app uses.

### 2. Install command-line tools

```sh
xcode-select --install
xcode-select -p
```

Expected:
```
/Applications/Xcode.app/Contents/Developer
```

If it points at `/Library/Developer/CommandLineTools` instead:
```sh
sudo xcode-select -s /Applications/Xcode.app/Contents/Developer
```

### 3. Clone the repository

```sh
mkdir -p ~/code && cd ~/code
git clone git@github.com:nsahmed23/SnoreDetector.git
cd SnoreDetector
git status
```

Expected: `On branch main`, `nothing to commit, working tree clean`.

If SSH auth fails: use the HTTPS URL
(`https://github.com/nsahmed23/SnoreDetector.git`) and a personal
access token, then fix SSH keys later.

### 4. Build the Rust xcframework

The Swift app links `SnoreGuardCore.xcframework`, produced from the
Rust crate in `rust/` via `cbindgen` + `xcodebuild -create-xcframework`.

```sh
./scripts/mac/bootstrap.sh
```

The bootstrap script (from PR #13 / `claude/mac-readiness`) installs
`xcodegen` if missing, adds the three Rust iOS targets via `rustup`,
validates `ios/project.yml`, and builds the xcframework.

Expected last line:
```
Bootstrap complete. Open ios/SnoreGuard.xcodeproj in Xcode.
```

If the script refuses to run: it is Linux-guarded — confirm `uname` is
`Darwin`. If `rustup` install fails: install manually
(<https://rustup.rs>) and re-run.

### 5. Generate the Xcode project

`bootstrap.sh` already runs `xcodegen generate`. Confirm:

```sh
ls ios/SnoreGuard.xcodeproj
```

If the directory does not exist:
```sh
cd ios && xcodegen generate
```

### 6. Build for the iOS simulator

```sh
./scripts/mac/build-sim.sh
```

Expected: `BUILD SUCCEEDED` from `xcodebuild`.

If the build fails on Swift compile errors: capture the *first* error
line — that is almost always the real cause. The recovery path is
**step 7**, not "edit until it compiles."

### 7. Fix compile errors

The first compile is the real moment of truth. Likely failure classes
and fixes:

- **`'SgStatus.Ok' is inaccessible`** — wrap raw-value access. The
  earlier orchestration already converted hot spots; if a new one shows
  up, mirror the pattern in the rest of the codebase.
- **Missing module `SnoreGuardCore`** — xcframework didn't make it into
  the build phase. Re-run `bootstrap.sh`; verify
  `ios/SnoreGuard/Frameworks/SnoreGuardCore.xcframework` exists and
  that Xcode shows it under "Frameworks, Libraries, and Embedded
  Content."
- **`@MainActor` isolation errors** — usually a closure capture in
  `SyncManager` or `AudioClipSync`. Add `@MainActor in` to the closure
  or hop via `await MainActor.run { ... }`.
- **Codable mismatches between Swift and Go** — compare the failing
  type's `CodingKeys` against the Go struct tags in
  `backend/internal/server/{sessions,clips}.go`. The OpenAPI spec
  (`openapi/snoreguard.v1.yaml`) is the source of truth.

Fix one error at a time; rebuild between fixes. Do not batch-edit.

### 8. Run unit tests

```sh
./scripts/mac/test-sim.sh
```

Expected: green test count covering `SnoreGuardTests` (networking,
persistence, audio engine proxy) and `SnoreGuardCoreTests` if present.

Failures here are real — they did not show up on Linux because the
Linux CI does not build for iOS. Triage in this order: networking
(easiest, no Apple frameworks), persistence (Core Data store), audio
(may require a simulator with a microphone permission grant).

### 9. Configure code signing

In Xcode: select the `SnoreGuard` target → Signing & Capabilities →
Team → choose your Apple Developer team. Bundle Identifier must read
exactly `com.nsahmed.snoreguard`. Enable automatic signing.

Required capabilities (already in
`ios/SnoreGuard/SnoreGuard.entitlements`):
- HealthKit
- Background Modes → Audio, AirPlay, and Picture in Picture
- Push Notifications (only if v1 includes server-driven notifications;
  defer otherwise)

If Xcode complains about a missing provisioning profile: let it
auto-create one — that is what automatic signing is for.

### 10. Run on a physical iPhone

Plug in a physical iPhone over USB. Trust the Mac when prompted. In
Xcode's run-destination picker, select your device.

```
Cmd-R
```

Expected: app installs and launches on the device. The first launch
will prompt for microphone permission and (when the user opens the
HealthKit toggle) for HealthKit permissions.

If the device is grayed out: open Settings → Privacy & Security →
Developer Mode on the iPhone, enable, reboot, retry.

### 11. Test the microphone path

In the app: Record tab → tap Start. Speak / snore softly. The level
meter and ripple visualizer must respond. Stop. The session should
appear in the History tab with a non-zero event count if anything
crossed the threshold.

If the level meter is flat: microphone permission was denied. Reset via
Settings → Privacy & Security → Microphone → SnoreGuard → toggle on.
If it stays flat after grant: the `AVAudioEngine` install-tap path is
failing — check the device console for `AVAudioEngine` errors.

### 12. Test HealthKit toggles

Settings tab → flip the three HealthKit toggles one at a time:

1. **Read sleep** — should prompt for sleep-analysis read permission;
   on grant, the Insights tab should pull last night's `inBed` window
   if the device has any.
2. **Write inBed sessions** — when a recording session ends, an
   `HKCategoryType.sleepAnalysis(value: .inBed)` sample should be
   written for the session's wall-clock window. Verify in Apple's
   Health app → Browse → Sleep.
3. **Write audio exposure (uncalibrated)** — when enabled, recording
   sessions should write to `HKQuantityType.environmentalAudioExposure`
   with the *uncalibrated* note. Verify in Health app → Browse →
   Hearing → Environmental Sound Levels.

If any prompt does not appear: the matching usage-description string
is missing from `Info.plist`. Add it, rebuild, retry.

### 13. Test against local backend

In a second terminal tab on the Mac:

```sh
cd backend
docker compose --profile app up -d
./scripts/dev/smoke-backend.sh --mock-auth
```

Expected: 18-step smoke completes with `OK` on every step.

In the app: Settings → set the API base URL to `http://localhost:8080`
(or whatever the build configuration exposes), enable cloud sync,
trigger a manual sync. The History tab's per-session badge should flip
from "local" to "synced."

If the smoke fails: read the first failing step's curl output. The
script (`scripts/dev/smoke-backend.sh`) is intentionally verbose.

### 14. Test synthetic clip upload

Still pointed at local backend. In the app: Settings → enable raw audio
upload (this is the second opt-in). Trigger a recording short enough to
emit at least one clip. Watch the device console for the
`AudioClipSync` log line confirming a multipart POST to `/audio/clips`.

Verify on the backend side:
```sh
docker compose exec postgres psql -U snoreguard -c "SELECT id, sha256, byte_size FROM audio_clips ORDER BY created_at DESC LIMIT 5;"
```

A new row with a non-null `sha256` and a non-zero `byte_size` confirms
the round-trip.

If the upload returns 400 with a sha256 mismatch: the iOS-side hashing
disagrees with the backend's `streamingsha256`. Both sides should
chunk-hash; check `Crypto/Sha256+File.swift` is being used end-to-end.

### 15. Test the real audio capture path

This is the step the orchestrator could not verify on Linux: the path
from `AVAudioEngine` tap → audio file on disk → `ClipFileStore` →
`AudioClipSync`. Symptoms of a broken path:

- Clip uploads succeed but the bytes are silence.
- `byte_size` is consistently small (header-only buffers).
- The downloaded clip plays as static.

Run an end-to-end check: record a short session with a clear known
sound (clap, voice). Upload via cloud sync. Download the clip back
through `GET /audio/clips/{id}/download` (the in-app "play remote
clip" path, or `curl` against the API). Listen. The download should
match what was recorded.

If the audio is silence or static: this is the **first product-critical
implementation gap** to close after first compile. The clip-write
path's encoding (PCM vs AAC) and sample-rate alignment with
`AVAudioEngine`'s tap format are the two most likely culprits.

### 16. Archive

In Xcode: Product → Archive. Wait for the archive to build (longer
than a regular build because Xcode compiles in Release configuration
and strips debug symbols).

When the Organizer opens with the new archive: verify the version and
build number are not duplicates of a prior submission. Bump
`MARKETING_VERSION` / `CURRENT_PROJECT_VERSION` in
`ios/project.yml` if needed and re-archive.

If the archive fails on a code-signing error: this is almost always a
mismatched provisioning profile. Re-confirm step 9.

### 17. Upload to TestFlight

In the Organizer: select the archive → Distribute App → App Store
Connect → Upload. Wait for processing in App Store Connect (5–30 min
typical).

Once processed, the build appears in TestFlight. Add yourself as an
internal tester. Install via the TestFlight app on the iPhone. Run the
golden-path flow once more on the TestFlight build to confirm the
release-mode build behaves like the debug build.

If processing fails with `Invalid Bundle` or a missing usage-description
error: App Store Connect emails a precise reason — fix the named
issue, archive again, re-upload.

---

## Definition of done

The first Mac session is **complete** when, and only when, all of the
following are true:

- Steps 1–9 succeeded (build green on simulator + signing configured).
- Steps 10–12 succeeded on a physical device (microphone, HealthKit
  read, HealthKit write inBed, HealthKit write audio exposure).
- Step 13 succeeded against local backend (sessions sync, events sync).
- Step 14 succeeded with a synthetic clip uploaded and persisted.
- Step 15 was attempted and the result (pass / fail / partial) is
  written down.
- Step 16 produced an archive without code-signing errors.
- Step 17 produced a TestFlight build that ran on at least one device.

If step 15 fails: that is acceptable for the first session — note it
explicitly, do not pretend it passed, and treat it as the next product
priority.

---

## What this runbook deliberately does **not** cover

- **GCP production deploy** (Cloud Run service config, Cloud SQL
  provisioning, Artifact Registry images, Secret Manager entries).
  That happens in a separate session after the local Mac path is
  green. See [`DEPLOYMENT.md`](./DEPLOYMENT.md).
- **Background-delivery HealthKit observers, Apple Watch companion,
  Zig audio-lab CLI** — all already deferred per the roadmap.
- **App Store submission** beyond TestFlight — privacy nutrition
  labels, screenshots, App Review answers come after the first
  TestFlight build is stable.
- **Real production GCS upload** — the first Mac session points the
  app at local backend, not Cloud Run, so the GCS adapter does not
  need to be exercised on day one.
