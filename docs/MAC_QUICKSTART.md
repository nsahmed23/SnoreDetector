# Mac quickstart

Audience: a senior engineer with Xcode 15.4+ installed who wants to take
SnoreGuard from a fresh `git clone` to TestFlight on their first
sitting at a Mac. Each section gives you the exact command and what to
expect when it works.

For the architectural picture see
[`NATIVE_IOS_PORT_PLAN.md`](./NATIVE_IOS_PORT_PLAN.md). For the medical
disclaimer the app must show, see [`DISCLAIMER.md`](./DISCLAIMER.md).
The pre-submission gate is in
[`RELEASE_CHECKLIST.md`](./RELEASE_CHECKLIST.md).

---

## 1. Prerequisites

Confirm each tool is installed and on PATH before going further. The
bootstrap script tries to install the easy ones for you, but it cannot
install Xcode or sign you up for the developer program.

| Tool | Min version | How to check | How to install |
|---|---|---|---|
| Xcode | 15.4 | `xcodebuild -version` | Mac App Store |
| Xcode command-line tools | matching Xcode | `xcode-select -p` | `xcode-select --install` |
| Apple Developer Program | active membership | <https://developer.apple.com/account> | enroll ($99/yr) |
| Homebrew | any recent | `brew --version` | <https://brew.sh> |
| `make` | any | `make --version` | ships with Xcode CLT |
| `rustup` + stable toolchain | 1.76+ | `rustup --version && rustc --version` | `bootstrap.sh` installs |
| Docker Desktop | 4.x | `docker --version` | <https://www.docker.com/products/docker-desktop> |

Expected `xcodebuild -version`:

```
Xcode 15.4
Build version 15F31d
```

---

## 2. One-time bootstrap

Installs `xcodegen`, adds the three Rust iOS targets, validates
`ios/project.yml`, builds `SnoreGuardCore.xcframework`, and runs
`xcodegen generate` to produce `ios/SnoreGuard.xcodeproj`. Idempotent —
re-running it is safe.

```
cd ios && ./../scripts/mac/bootstrap.sh
```

Expected last line:

```
Bootstrap complete. Open ios/SnoreGuard.xcodeproj in Xcode.
```

If `xcodegen` is missing, the script will `brew install xcodegen` for
you. If `rustup` is missing, the script will run the official
installer. The script refuses to run on Linux.

---

## 3. Local backend

Postgres in Docker, the three Go services on the host. From a separate
terminal tab:

```
cd backend
docker compose up -d postgres
make run-sync
```

Expected `make run-sync`:

```
go run ./cmd/sync-service
{"level":"info","msg":"sync-service listening","addr":":8080"}
```

For the full stack (analytics on `:8081` and export on `:8082`) start
each in its own terminal:

```
make run-analytics      # :8081
make run-export         # :8082
```

Or, equivalently, bring everything up via compose:

```
docker compose --profile app --profile otel up -d
```

Smoke-test the sync service:

```
curl -sS localhost:8080/healthz
```

Expected:

```
{"status":"ok","time":"2026-05-06T12:34:56Z"}
```

---

## 4. Simulator build

```
cd ios && ./../scripts/mac/build-sim.sh
```

Expected last line:

```
** BUILD SUCCEEDED **
```

The script defaults `DESTINATION` to `platform=iOS Simulator,name=iPhone 15 Pro`.
Override it with `DESTINATION="platform=iOS Simulator,name=iPhone 15"`.

---

## 5. Simulator run

```
open ios/SnoreGuard.xcodeproj
```

In Xcode:

1. Pick the **iPhone 15 Pro** simulator from the run-destination
   dropdown.
2. Hit **⌘R**.

Expected: the app boots straight to the Record tab. The level meter
sits at the noise floor; tapping **Start listening** prompts for the
mic permission (simulator microphones in macOS feed host-machine
audio).

---

## 6. Run unit tests

```
cd ios && ./../scripts/mac/test-sim.sh
```

Expected last lines:

```
Test Suite 'All tests' passed at <date>.
** TEST SUCCEEDED **
```

---

## 7. Physical device run

A device build needs your team certificate and a provisioning profile
that includes the HealthKit and App-Group capabilities.

1. Plug in the device, unlock it, and trust the host Mac.
2. In Xcode select the **SnoreGuard** target → **Signing & Capabilities**.
3. Set **Team** to your Apple Developer team. Toggle
   **Automatically manage signing** on if it isn't already.
4. Confirm the capabilities list contains: HealthKit, App Groups
   (`group.com.snoreguard.shared`), and Sign in with Apple.
5. Pick the device from the run-destination dropdown.
6. Hit **⌘R**.

> **Note:** `com.apple.developer.healthkit` and the App-Group
> entitlement need to be added to the App ID in the developer portal
> *before* Xcode can build a profile that includes them. If signing
> fails with `Provisioning profile doesn't include the
> com.apple.developer.healthkit entitlement`, open
> <https://developer.apple.com/account/resources/identifiers/list>,
> edit the `com.snoreguard.app` App ID, tick the capabilities, then
> back in Xcode click **Try again** on the signing error.

---

## 8. Verify microphone

First launch on a real device prompts for microphone permission.

1. Tap **Allow** on the system sheet ("SnoreGuard needs to access your
   microphone to detect snoring.").
2. Tap **Start listening**.
3. Talk into the mic; the level meter should rise immediately.

Expected: the level meter responds within ~100 ms; the ripple
visualizer animates in time with detected events.

If the meter stays flat, check **Settings → SnoreGuard → Microphone**
on the device — permission may have been denied.

---

## 9. Verify HealthKit

1. Open **Settings** in the app.
2. Toggle **Read sleep stages from Apple Health** → grant on the
   system sheet.
3. Toggle **Write inBed sessions to Apple Health** → grant.
4. Toggle **Write uncalibrated audio-exposure samples** → grant.
5. Record a short session.
6. Open Apple's **Health** app → **Browse → Sleep**. The session you
   just recorded shows up under "In Bed". Under **Hearing → Headphone
   Audio Levels** you should see the audio-exposure samples (clearly
   labeled in the app as *uncalibrated*).

Each toggle is independent — denying one doesn't affect the others.
You can revoke all three from **Settings → Health → Data Access &
Devices → SnoreGuard** on the device.

---

## 10. Verify cloud sync

1. Find your Mac's LAN IP: `ipconfig getifaddr en0`.
2. Edit `ios/SnoreGuard/Network/APIConfig.swift` and set
   `baseURL = "http://<mac-ip>:8080"`.
3. Build and run on device.
4. In **Settings → Account**, tap **Sign in with Apple**. Approve.
5. Toggle **Cloud sync** on.
6. Record a session; tap **Sync now** (or wait for the auto-sync timer).
7. On the Mac:

```
psql postgres://snoreguard:snoreguard@localhost:5432/snoreguard \
  -c 'SELECT count(*) FROM events;'
```

Expected: the row count matches the number of events the device just
synced. The same query against `sessions` shows your latest session.

---

## 11. Verify audio clip upload

1. In **Settings**, leave **Cloud sync** on and additionally toggle
   **Upload audio clips** on.
2. Record a session loud enough to fire several events.
3. Tap **Sync now**.
4. On the Mac, list the local blob store:

```
ls -lh backend/var/blobstore/clips/<user_id>/
```

Expected: one file per uploaded clip, each a few hundred KB to a few
MB. The filenames are object IDs.

5. Tail the sync-service log while syncing:

```
tail -f backend/var/log/sync-service.log | grep -E 'clip|sha256'
```

Expected: each upload logs a line of the form
`{"msg":"clip stored","object_id":"…","sha256":"…","size":NNNN}`. The
`sha256` is computed over the multipart body before the blob hits disk.

---

## 12. TestFlight upload

1. In Xcode, select **Any iOS Device (arm64)** as the run destination.
2. **Product → Archive** (this builds a Release configuration; takes
   1-3 minutes).
3. When the Organizer opens, click **Distribute App** → **App Store
   Connect** → **Upload** → keep the defaults → **Next** → **Upload**.
4. After processing, the build appears in App Store Connect under
   **TestFlight → iOS Builds**. Add it to a tester group; testers get
   an email + push notification within ~10 minutes.

Pre-submission criteria (privacy labels, screenshots, release notes,
etc.) are tracked in [`RELEASE_CHECKLIST.md`](./RELEASE_CHECKLIST.md).
Do not click "Submit for review" until that checklist is fully green.
