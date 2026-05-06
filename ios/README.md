# SnoreGuard iOS app

Native iOS port of the React prototype. SwiftUI front end, AVAudioEngine
audio path, with the detection algorithm coming from the Rust crate in
[`rust-core/`](../rust-core/) via a C ABI.

> **Phase 2 status — written blind.** This branch was authored on a
> Linux sandbox without Xcode access. The Swift sources, the
> `project.yml`, and the build wiring are believed correct against
> Apple's documented APIs but **have not been compiled or run.**
> Treat the first build on macOS as the verification step. See
> [Verification checklist](#verification-checklist) below.

## Layout

```
ios/
├── project.yml                       XcodeGen project spec (source of truth)
├── README.md                         this file
├── SnoreGuard/                       app target
│   ├── App.swift                     @main entrypoint, owns SettingsStore
│   ├── ContentView.swift             three-tab container
│   ├── Audio/
│   │   ├── SnoreCore.swift           Swift wrapper around the Rust C ABI
│   │   └── AudioEngine.swift         AVAudioEngine → Float32 16kHz mono frames
│   ├── Models/
│   │   ├── DetectorSettings.swift
│   │   └── RecordingSession.swift
│   ├── Networking/                   sync-service client (phase 2-B)
│   │   ├── Endpoints.swift           typed Endpoint protocol + concrete endpoints
│   │   ├── APIClient.swift           actor-isolated URLSession client + retry/refresh
│   │   ├── AuthManager.swift         Sign in with Apple, single-flight refresh
│   │   ├── TokenKeychain.swift       SecItem wrapper for access + refresh tokens
│   │   └── EventSync.swift           upload/pull coordinator
│   ├── ViewModels/
│   │   └── RecorderViewModel.swift   @MainActor; wires engine ↔ core ↔ UI
│   ├── Views/
│   │   ├── RecordTabView.swift
│   │   ├── HistoryTabView.swift      placeholder (real history → phase 5)
│   │   ├── SettingsTabView.swift
│   │   ├── LevelMeterView.swift
│   │   ├── DisclaimerBanner.swift
│   │   └── ConsentSheet.swift
│   ├── Persistence/
│   │   └── SettingsStore.swift       UserDefaults-backed
│   └── Resources/Assets.xcassets/    AppIcon + AccentColor placeholders
└── SnoreGuardTests/
    ├── SnoreCoreTests.swift          FFI lifecycle + sustained-input event
    ├── DetectorSettingsTests.swift   slider snap + sensitivity rawValues
    └── Networking/
        └── APIClientTests.swift      URLProtocol-mocked happy path, 401-retry, 429, single-flight
```

## Build flow

The project uses [XcodeGen](https://github.com/yonaskolb/XcodeGen) to
generate `SnoreGuard.xcodeproj` from `project.yml`. The generated
project is gitignored; checking it in would mean re-resolving merge
conflicts every time we touch sources.

```bash
brew install xcodegen          # one-time
cd ios
xcodegen generate              # produces SnoreGuard.xcodeproj
open SnoreGuard.xcodeproj
```

### Prerequisite: build the Rust core's xcframework

`SnoreGuard` links `SnoreGuardCore.xcframework`, produced from the
Rust crate in [`rust-core/`](../rust-core/). The xcframework is **not**
committed.

```bash
cd ../rust-core
rustup target add aarch64-apple-ios aarch64-apple-ios-sim x86_64-apple-ios
./scripts/build-xcframework.sh
# → ../rust-core/SnoreGuardCore.xcframework
```

Once that exists, `xcodegen generate` (or just an Xcode rebuild) will
pick it up via the `FRAMEWORK_SEARCH_PATHS` setting in `project.yml`.

## Architecture (one-screen overview)

```
                ┌─────────── @MainActor ───────────┐
                │                                  │
   AVAudioEngine tap                       SwiftUI view layer
   (render thread)                              (TabView)
        │                                          ▲
        │ 256 Float32 samples                      │ @Published state
        ▼                                          │
   AudioEngineProxy                          RecorderViewModel
   (closure → MainActor hop)                       │
                                                   │ FFI calls
                                                   ▼
                                              SnoreCore  ──── opaque pointer ───▶  Rust detector
```

Threading guarantees:
- All FFI calls (`sg_detector_*`) happen on `@MainActor` — the proxy
  hops the render thread's frame back to main before touching
  `SnoreCore`. Cheap because the FFI work is microseconds.
- The detector itself is not Sync; this confinement is required.
- A 30 Hz `Timer.publish` polls events as a safety net even if
  `pushFrame` returned `false` (rare, but possible if a frame ends
  exactly at an event boundary).

Audio session:
- `.playAndRecord` + `.measurement` mode + `.mixWithOthers` so the
  user's alarm can still play and we don't kick out other audio.
- `audio` is the only entry in `UIBackgroundModes` for now —
  `processing` / `remote-notification` etc. land in later phases.

## Networking

Phase 2-B adds the iOS client for the merged Phase-1 sync-service
(`backend/cmd/sync-service`). All network code lives under
`SnoreGuard/Networking/`. The auth flow:

1. **Sign in with Apple** — `AuthManager.signIn(with:)` takes an
   `ASAuthorizationAppleIDCredential`, extracts its `identityToken`
   (an Apple-signed JWT), and POSTs it to `/auth/apple`. The backend
   verifies the token against Apple's JWKS and responds with our own
   access + refresh token pair.
2. **Token storage** — Both tokens, the user ID, and the access-token
   expiry land in the iOS Keychain via `TokenKeychain`. We use direct
   `SecItem*` calls (no third-party wrapper) with
   `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly` so tokens
   survive backgrounding but never roam via iCloud Keychain.
3. **Auto-refresh** — `APIClient.request` attaches `Authorization:
   Bearer <access>` for any endpoint with `requiresAuth: true`. On
   `401` the client calls `AuthManager.refresh()` once and replays
   the original request. Pre-emptively, `currentAccessToken()` will
   trigger a refresh if the cached token expires within the next 60 s.
4. **Single-flight refresh** — Concurrent callers that all see a 401
   coalesce on a single `Task<AuthTokensResponse, Error>` cached on
   the `AuthManager`. This prevents two refreshes burning two refresh
   tokens (and triggering the backend's family-revocation theft
   detection on the loser).
5. **Sign-out** — Best-effort POST to `/auth/logout` (revokes access
   JTI + refresh family server-side), then Keychain wipe regardless
   of network outcome.

The non-auth surface:

- `EventSync.upload(events:sessionStartedAt:)` — converts in-memory
  `[SnoreEvent]` (relative `start_ms`) to `[WireSnoreEvent]`
  (absolute `started_at`) and bulk-POSTs to `/events`. Backend dedupes
  on `client_event_id` so retries are no-ops.
- `EventSync.pull(since:)` — pages through `GET /events?cursor=…`
  using the backend's opaque compound cursor (`base64url(rfc3339nano +
  "|" + uuid)`). Cursor is never parsed client-side.

### Wire shapes

| iOS type                    | JSON shape                                                                           |
|-----------------------------|--------------------------------------------------------------------------------------|
| `AuthTokensResponse`        | `{user_id, access_token, access_token_expires_at, refresh_token, refresh_token_expires_at}` |
| `WireSnoreEvent`            | `{client_event_id, started_at, duration_ms, avg_db, session_id?}`                    |
| `PostEventsResponse`        | `{inserted, received}`                                                               |
| `ListEventsResponse`        | `{events: [...], next_cursor?}`                                                      |

Dates use ISO8601; the decoder accepts both fractional-second
(`2026-05-06T13:00:00.123456789Z`) and whole-second (`...:00Z`)
forms because Go's `time.Time` JSON marshaller emits both.

### Errors

All non-2xx, non-429 responses surface as `APIError.server(statusCode,
message)`. The `message` is decoded from the backend's standard
`{"error": "..."}` envelope when available. 429 surfaces as
`APIError.rateLimited(retryAfterSeconds:)` parsed from the
`Retry-After` header.

### Test approach

`SnoreGuardTests/Networking/APIClientTests.swift` uses a
`URLProtocol` stub (`MockURLProtocol`) registered on a custom
`URLSessionConfiguration.ephemeral` — no live network. The cases:

- `testAuthApple_HappyPath` — round-trips a 200 response, asserts
  Keychain persistence and User-Agent / Content-Type header attachment.
- `test401_TriggersRefreshOnce` — verifies that an authenticated call
  receiving 401 fires exactly one refresh and replays the original
  request (3 total HTTP hops).
- `test429_ThrowsRateLimited` — asserts `Retry-After` parsing.
- `testConcurrentRefresh_IsSingleFlight` — two concurrent 401-bound
  requests coalesce on a single refresh round-trip.
- `testServerErrorEnvelope_IsSurfaced` — `{"error": "..."}` body is
  decoded into `APIError.server.message`.

### Configuration

`APIConfig.default` points at `http://localhost:8080` for local dev
against `make run-sync` from `backend/`. Production base URL is a
build-time concern; the default ships unchanged until we have a
TestFlight build to point at.

### Out of scope (this phase)

- Wiring `RecorderViewModel` to call `EventSync.upload` on session
  end — Phase 2-C will add the offline buffer + retry policy.
- A persistent `EventStore` for the History tab to read pulled events.
- Watch-app token sharing via `kSecAttrAccessGroup`.

## Verification checklist

- [ ] `xcodegen generate` produces a clean `SnoreGuard.xcodeproj`.
- [ ] **Build** the project for an iOS Simulator (e.g. iPhone 15 Pro).
      The first build commonly surfaces:
  - missing optional `import` (e.g. `Combine`, `OSLog`)
  - SwiftUI-API drift between SDK versions (the project targets iOS
    17; some `@Observable` / `Bundle` extensions may need tweaking on
    iOS 18+)
  - Missing `module.modulemap` in the xcframework (run the build
    script with iOS 17+ SDK)
- [ ] **Run** the app in the simulator; the consent sheet must appear,
      then the Record tab.
- [ ] **Run on a device.** Microphone permission prompts; tapping
      "Start listening" begins the engine and the level meter responds.
- [ ] **Run unit tests** (`xcodebuild test -scheme SnoreGuard \
      -destination 'platform=iOS Simulator,name=iPhone 15 Pro'`).
      All cases in `SnoreGuardTests` should pass.
- [ ] Confirm a **lock-screen test**: start recording, lock the
      device, observe that detection continues (the `audio` background
      mode is enabled).

## Out of scope (future phases)

- **Persistent history** (Core Data / SwiftData) — phase 5 of the port plan.
- **HealthKit** read/write — phase 3 (`claude/ios-healthkit`).
- **Backend sync** (the Go services in `backend/`) — phase 4/5
  already shipped on the backend side; the iOS network client lands
  in a follow-up.
- **Real app icon** + launch storyboard — placeholder JSONs only.
- **Localizations** beyond English — `developmentLanguage: en` is the
  only entry today.
- **App Store metadata** + privacy nutrition labels — addressed
  alongside the first TestFlight build.

## Why no `.xcodeproj` is committed

Manually-curated `.pbxproj` files conflict on every PR that touches
file membership. XcodeGen treats `project.yml` as the source of
truth and regenerates the project deterministically, so the generated
file stays out of git. New files added in this directory tree are
auto-discovered on the next `xcodegen generate`.
