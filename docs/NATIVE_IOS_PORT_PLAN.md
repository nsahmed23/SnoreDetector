# Native iOS Port Plan

This is the build plan for the real SnoreGuard product: a native iOS app written in Swift, with the detection algorithm in a Rust static library, and backend services in Go. The web app in `src/` stays as the visual product spec (see `PRODUCT_SPEC.md`).

---

## Target architecture

```
┌──────────────────────────────────────────────┐
│  SwiftUI app (ios/SnoreGuard)                │
│  ┌─────────────────────────────────────────┐ │
│  │ Views: Record / Insights / Settings     │ │
│  │ ViewModels: @Observable session state   │ │
│  │ Audio: AVAudioEngine + AVAudioSession   │ │
│  │ Health: HealthKitClient (HKHealthStore) │ │
│  └────────────────┬────────────────────────┘ │
│                   │ Swift wrapper            │
│  ┌────────────────▼────────────────────────┐ │
│  │ SnoreGuardCore.xcframework              │ │
│  │ (C-ABI bridge → Rust staticlib)         │ │
│  └────────────────┬────────────────────────┘ │
└───────────────────┼──────────────────────────┘
                    │ HTTPS
       ┌────────────▼─────────────┐
       │ Go services (backend/)   │
       │  sync / analytics /      │
       │  export / observability  │
       └──────────────────────────┘
```

The mobile app is **fully functional offline**. Backend is optional: cross-device sync, server-generated insights, and PDF reports.

---

## iOS app skeleton (`ios/SnoreGuard/`)

Proposed Xcode project layout (created in a follow-up branch):

```
ios/
├── SnoreGuard.xcodeproj/
└── SnoreGuard/
    ├── App.swift                    @main, root TabView
    ├── Info.plist                   NSMicrophoneUsageDescription, HealthKit usage strings
    ├── SnoreGuard.entitlements      HealthKit, BackgroundModes(audio)
    ├── Features/
    │   ├── Record/
    │   │   ├── RecordView.swift
    │   │   ├── RecordViewModel.swift
    │   │   └── RippleVisualizer.swift
    │   ├── Insights/
    │   │   ├── InsightsView.swift
    │   │   ├── DailyChart.swift
    │   │   ├── TrendChart.swift
    │   │   └── ExportSheet.swift
    │   ├── Settings/
    │   │   ├── SettingsView.swift
    │   │   └── DisclaimerView.swift
    │   └── Onboarding/
    │       └── OnboardingView.swift
    ├── Audio/
    │   ├── AudioEngine.swift        AVAudioEngine wrapper, frame downsample to 16 kHz mono
    │   └── SessionConfigurator.swift AVAudioSession setup
    ├── Health/
    │   └── HealthKitClient.swift
    ├── Core/
    │   ├── SnoreCore.swift          Swift wrapper around SnoreGuardCore.xcframework
    │   └── SnoreCoreError.swift
    └── Resources/
        ├── Assets.xcassets
        └── Localizable.strings
```

`AudioEngine` taps `AVAudioEngine.inputNode`, downsamples to mono 16 kHz, and pushes 256-sample `Float32` frames into `SnoreCore.pushFrame(_:)`. A `Combine` publisher (or `AsyncStream`) emits detected events back to `RecordViewModel`.

---

## Rust algorithm core (`rust-core/snoreguard-core/`)

A `cargo` crate built as a `staticlib` and packaged into a fat `XCFramework`.

### Crate setup

```toml
# rust-core/snoreguard-core/Cargo.toml
[package]
name    = "snoreguard-core"
version = "0.1.0"
edition = "2021"

[lib]
crate-type = ["staticlib"]

[dependencies]
# none for v1; std-only port of the heuristic
```

### Public C-ABI surface

Designed for FFI safety: opaque pointer, single-producer/single-consumer ring buffer, integer error codes.

```rust
// rust-core/snoreguard-core/src/lib.rs (sketch)
#[repr(C)]
pub struct SgDetector { /* private */ }

#[repr(C)]
pub struct SgEvent {
    pub start_ms:    u64,
    pub duration_ms: u32,
    pub avg_db:      f32,
}

#[repr(u8)]
pub enum SgSensitivity { Low = 0, Medium = 1, High = 2 }

#[no_mangle] pub extern "C" fn sg_detector_new(
    threshold_db: f32, sensitivity: SgSensitivity, sample_rate_hz: u32
) -> *mut SgDetector;

#[no_mangle] pub extern "C" fn sg_detector_push_frame(
    d: *mut SgDetector, samples: *const f32, len: usize
) -> i32;

#[no_mangle] pub extern "C" fn sg_detector_poll_event(
    d: *mut SgDetector, out: *mut SgEvent
) -> i32;        // 1 = event written, 0 = none, <0 = error

#[no_mangle] pub extern "C" fn sg_detector_set_threshold(d: *mut SgDetector, db: f32) -> i32;
#[no_mangle] pub extern "C" fn sg_detector_set_sensitivity(d: *mut SgDetector, s: SgSensitivity) -> i32;
#[no_mangle] pub extern "C" fn sg_detector_free(d: *mut SgDetector);
```

### Tier-2 iOS targets

Rust officially supports the iOS targets but classifies them as **Tier 2 without host tools** — cross-compilation requires the iOS SDK from Xcode.

| Target | Use |
|---|---|
| `aarch64-apple-ios` | Real iPhone / iPad |
| `aarch64-apple-ios-sim` | Apple-Silicon simulator |
| `x86_64-apple-ios` | Intel-Mac simulator (legacy) |

### Build → xcframework

Cross-compile each target with the Xcode iOS SDK:

```sh
rustup target add aarch64-apple-ios aarch64-apple-ios-sim x86_64-apple-ios

# Device
SDKROOT=$(xcrun --sdk iphoneos --show-sdk-path) \
  cargo build --release --target aarch64-apple-ios

# Simulators (lipo into one fat staticlib)
SDKROOT=$(xcrun --sdk iphonesimulator --show-sdk-path) \
  cargo build --release --target aarch64-apple-ios-sim
SDKROOT=$(xcrun --sdk iphonesimulator --show-sdk-path) \
  cargo build --release --target x86_64-apple-ios
lipo -create \
  target/aarch64-apple-ios-sim/release/libsnoreguard_core.a \
  target/x86_64-apple-ios/release/libsnoreguard_core.a \
  -output target/sim/libsnoreguard_core.a

# Header generation
cbindgen --config cbindgen.toml --crate snoreguard-core \
  --output include/snoreguard_core.h

# Assemble xcframework
xcodebuild -create-xcframework \
  -library target/aarch64-apple-ios/release/libsnoreguard_core.a \
    -headers include \
  -library target/sim/libsnoreguard_core.a \
    -headers include \
  -output SnoreGuardCore.xcframework
```

A small `Makefile` or shell script in `rust-core/` will wrap this. Xcode runs it as a build phase before linking.

---

## Swift ↔ Rust FFI boundary

- The bridging header (`SnoreGuard-Bridging-Header.h`) imports `snoreguard_core.h`.
- `SnoreCore.swift` is the only Swift type that touches raw C pointers. It owns a `OpaquePointer` and frees it in `deinit`. `SnoreCore` is `Sendable` only on a confined actor — the underlying ring buffer is single-producer/single-consumer.
- Errors are mapped:
  ```swift
  enum SnoreCoreError: Int32, Error {
      case nullPointer = -1
      case invalidLength = -2
      case unknown = -99
  }
  ```
- `pushFrame` accepts `UnsafePointer<Float>` from the audio tap; no allocation per frame.
- `pollEvent` is called by `RecordViewModel` on a timer (or after each tap callback) and yields events into the UI stream.

### Threading contract

- **Producer:** `AVAudioEngine` input tap (real-time audio thread).
- **Consumer:** UI/event thread, polling on a 30 Hz timer.
- The Rust ring buffer is lock-free SPSC. Crossing this boundary is the only place threading rules matter.

---

## Audio session strategy

```swift
let session = AVAudioSession.sharedInstance()
try session.setCategory(
    .playAndRecord,
    mode: .default,
    options: [.mixWithOthers, .allowBluetoothA2DP, .defaultToSpeaker]
)
try session.setActive(true)
```

- `mixWithOthers` is non-negotiable — users listen to audiobooks on AirPods while tracking.
- Subscribe to `routeChangeNotification` to handle AirPods disconnects gracefully.
- Background mode `audio` in `Info.plist` keeps the engine alive when locked.
- Tap on `inputNode` at the device's preferred format, then convert to mono 16 kHz `Float32` via `AVAudioConverter`.

---

## HealthKit strategy

- Read scope: `HKCategoryType(.sleepAnalysis)`.
- Write scope: a custom `HKQuantitySample` representing nightly snoring duration and average intensity, with metadata (`["session_id", "avg_db"]`). Decision on whether to wrap the night as an `HKWorkout` is deferred (see `PRODUCT_SPEC.md` open questions).
- Authorization is requested lazily on first toggle — the Settings switch reflects real `HKHealthStore.authorizationStatus(for:)`, not just a local boolean.
- Reads are performed via `HKSampleQuery` and `HKStatisticsQuery`; results power the Insights summary cards and the daily chart.

---

## Go backend services (`backend/`)

Optional services, consumed via REST. Auth via Apple Sign-In ID token verified server-side (`golang.org/x/oauth2/jws` or similar).

| Service | Responsibility | Stack sketch |
|---|---|---|
| `sync-service` | Cross-device sync of sessions, settings, retained clip metadata | `chi` router, `sqlc` + Postgres, `pgx` |
| `analytics-service` | Sleep + snore aggregation, percentile trends, weekly insight generation (the "Insight" card text) | Postgres + a small worker pool; optional LLM call for the natural-language insight |
| `export-service` | CSV / PDF report generation, signed S3 URLs | `go-pdf/fpdf`, S3 SDK |
| `observability` | OpenTelemetry collector + log shipping for the AI insight layer | Otel collector + Loki/Tempo |

CSV export remains client-only as the default path — the Go service is for richer PDF reports and for users who want to email reports without leaving the app.

---

## Build & release

- Rust core builds as a Build Phase script (`./scripts/build-core.sh`) that compiles the three targets and rebuilds `SnoreGuardCore.xcframework` if the crate changed.
- App is signed with the team's standard provisioning profile.
- TestFlight is the primary distribution channel during private beta.
- CI (separate branch): `cargo test`, `cargo clippy -- -D warnings`, `xcodebuild test -scheme SnoreGuard -destination 'platform=iOS Simulator,name=iPhone 15'`, `go test ./...`, `vite build`.

---

## Phasing

| Phase | Branch | Deliverable |
|---|---|---|
| **0** | `claude/snoreguard-ios-planning-tAxw8` (this PR) | Web cleanup, honest wording, planning docs, native folder stubs |
| **1** | `rust-core-skeleton` | `rust-core/snoreguard-core` Cargo crate with the heuristic ported from `useAudioMonitor.ts`, property tests, xcframework build script |
| **2** | `ios-record-tab` | Xcode project, SwiftUI scaffold mirroring three tabs, Record tab end-to-end (audio engine → Rust core → live UI) |
| **3** | `ios-healthkit` | Real HealthKit read/write replacing the mock toggle |
| **4** | `backend-sync` | `backend/sync-service` Go skeleton (chi + sqlc + Postgres migrations + Apple Sign-In) |
| **5** | `backend-analytics-export` | `backend/analytics-service`, `backend/export-service`, OTel observability |

Each phase is independently reviewable. No phase ships without the previous one merged.
