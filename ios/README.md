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
│   ├── ViewModels/
│   │   └── RecorderViewModel.swift   @MainActor; wires engine ↔ core ↔ UI
│   ├── Views/
│   │   ├── RecordTabView.swift
│   │   ├── HistoryTabView.swift      Core Data-backed list (phase 2-C)
│   │   ├── SettingsTabView.swift
│   │   ├── LevelMeterView.swift
│   │   ├── DisclaimerBanner.swift
│   │   └── ConsentSheet.swift
│   ├── Persistence/
│   │   ├── SettingsStore.swift       UserDefaults-backed
│   │   ├── PersistenceController.swift   Core Data stack (app-group SQLite)
│   │   ├── EventStore.swift          domain API: record/fetch/markSynced
│   │   └── SessionEntity.xcdatamodeld    Session ↔ Event schema
│   └── Resources/Assets.xcassets/    AppIcon + AccentColor placeholders
└── SnoreGuardTests/
    ├── SnoreCoreTests.swift          FFI lifecycle + sustained-input event
    ├── DetectorSettingsTests.swift   slider snap + sensitivity rawValues
    └── Persistence/
        └── EventStoreTests.swift     in-memory Core Data round-trips
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

## Persistence (Core Data)

Phase 2-C ships the on-device session history.

- **Schema** (`Persistence/SessionEntity.xcdatamodeld`): two entities,
  `SessionEntity` has-many `EventEntity` with cascade-delete on the
  parent. Schema versioning is enabled and lightweight migration is
  on (`shouldMigrateStoreAutomatically` + `shouldInferMappingModelAutomatically`)
  so additive schema changes don't require an explicit mapping model.
- **Storage location**: app-group container
  `group.com.snoreguard.shared` so a future Watch extension or share
  extension can read the same SQLite file. The app-group capability
  lives in `SnoreGuard/SnoreGuard.entitlements`. Provisioning the
  matching App ID + group in the Apple Developer portal is a Mac-side
  step — see the verification checklist below.
- **Layering**: `EventStore` is the only thing that touches
  `NSManagedObjectContext` directly. The view layer uses
  `HistoryViewModel` (Core Data values flow in via async
  `allSessions()`); `RecorderViewModel.stop()` calls
  `eventStore.record(session)` on a detached `Task` so the write
  happens on a background context off the main actor.
- **Tests**: `SnoreGuardTests/Persistence/EventStoreTests.swift`
  exercises insert + fetch-by-range + unsynced + markSynced against
  an in-memory store, so the suite stays fast and hermetic.

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

- **Persistent history** (Core Data) — landed in phase 2-C. See
  [Persistence](#persistence-core-data) below.
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
