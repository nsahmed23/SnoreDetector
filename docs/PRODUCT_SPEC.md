# SnoreGuard Product Spec

This document treats the React/Vite web prototype in `src/` as the **frozen product spec** for the native iOS build. The web app is a visual cockpit — its UX, copy, color palette, and information architecture define what the native app must deliver. The native app does *not* need to reuse any web code; it needs to reproduce the **experience**.

---

## Screens & flows

The prototype has four surfaces, in this order:

1. **Onboarding** (`src/components/OnboardingTutorial.tsx`) — three-step modal carousel shown on first run.
2. **Record** tab (`src/components/RecordTab.tsx`) — large play/stop button, live decibel meter with threshold marker, ripple visualizer, session stats (events / avg intensity / duration), HealthKit sync chip.
3. **Insights** tab (`src/components/InsightsTab.tsx`) — daily / weekly / monthly toggle, summary cards, sleep-stage chart with snore-event overlays, audio clip playback list grouped by sleep stage, CSV export modal, HealthKit daily sync card.
4. **Settings** tab (`src/components/SettingsTab.tsx`) — HealthKit toggle, AVAudioSession status indicator, threshold slider, detector sensitivity buttons, "How it works" callout, medical disclaimer footer.

Bottom navigation is three tabs: Sleep / Analytics / Settings. Container is locked to `max-w-md` to mimic an iPhone display.

---

## Feature parity matrix

| Prototype feature | Web implementation | Native iOS equivalent | Notes |
|---|---|---|---|
| Microphone capture | `navigator.mediaDevices.getUserMedia({ audio: true })` | `AVAudioSession` configured `.playAndRecord` with options `[.mixWithOthers, .allowBluetoothA2DP, .defaultToSpeaker]`; tap on `AVAudioEngine.inputNode` | Background-audio entitlement required; respect Bluetooth route changes |
| FFT analysis | `AnalyserNode` (fftSize 256, `getByteFrequencyData`) | `Accelerate.vDSP_fft_zrip` on a 256-sample mono float buffer | Push frames into Rust core; do not run heuristic in Swift |
| Snore detector heuristic | `useAudioMonitor` hook (low-frequency dominance + threshold + sustained-frame counter) | Rust `staticlib` exposed via C ABI; called from Swift wrapper | See `NATIVE_IOS_PORT_PLAN.md` for the FFI surface |
| Live volume meter & threshold marker | DOM divs with width % bound to volume state | SwiftUI `Canvas` driven by `TimelineView(.animation)` | Smooth at 60 fps from a published `@Observable` |
| Ripple visualizer (background) | Two animated circles scaled by current volume + state-driven color | SwiftUI `Circle().scaleEffect(...)` with `.animation(.easeOut)` | Match indigo→rose color shift on snore detection |
| Session stats (events / dB / duration) | React state in `RecordTab` | `@Observable` `RecordingSession` ViewModel | Reset to zero when tracking stops |
| Sleep-stage chart (daily) | `recharts` `AreaChart` with stepAfter + `ReferenceDot` per snore event | `Charts.AreaMark` (Swift Charts) + `Charts.PointMark` overlay | Snore dot color encodes sleep stage |
| Trend chart (weekly/monthly) | `recharts` `ComposedChart` (Bar + Line) | Swift Charts: `BarMark` + `LineMark` on dual axes | Use `.chartYAxis` `AxisMarks(position: .leading/.trailing)` |
| Sleep-stage summary cards | Static numbers in a grid | Same — populated from HealthKit aggregation | Swap mock numbers for real HealthKit queries |
| Audio clips by stage (playback) | Mock UI with `setTimeout(3000)` for progress | Real `AVAudioPlayer` over a rolling on-device buffer; clip extracted around each detected event | Retention controlled by user setting; never auto-uploaded |
| HealthKit toggle | `useState<boolean>` | `HKHealthStore.requestAuthorization` for read `.sleepAnalysis` and write a custom `HKQuantityType` (or `HKWorkout` per session) | Show real authorization status, not just a local toggle |
| HealthKit daily sync card | Static numbers | Pull from `HKStatisticsQuery` + `HKSampleQuery` | "Insight" copy generated server-side (see Go `analytics-service`) |
| CSV export | `data:text/csv` URI + click trick | `UIActivityViewController` with a temp file in `FileManager.default.temporaryDirectory` | Future PDF export goes through Go `export-service` for signed URLs |
| Threshold slider (30–90 dB) | HTML `<input type="range">` | SwiftUI `Slider(value:in:)` | Persist in `UserDefaults` |
| Detector sensitivity (low/medium/high) | Three-way button group | SwiftUI `Picker(.segmented)` | Pass to Rust core as `u8` enum across FFI |
| Onboarding | Modal carousel, 3 steps | SwiftUI `TabView(.page)` with `PageTabViewStyle` | Skippable; persist completion in `UserDefaults` |
| Background mix indicator | Static "Active" chip | Reflect actual `AVAudioSession.sharedInstance().category` and route | Update on `AVAudioSession.routeChangeNotification` |
| Medical disclaimer footer | Plain `<p>` in Settings | Same — SwiftUI `Text` with `.font(.caption2)` | Link to full `DISCLAIMER.md` page |

---

## Visual language

- **Background:** `slate-950` (`#020617`) base with an `#0a0f24` iPhone-cockpit container; two large blurred radial gradients (indigo and rose) for depth.
- **Primary accents:** indigo-600 (default), rose-500 (snore alert / over-threshold), emerald-500 (HealthKit OK).
- **Sleep stage colors:** Light → teal-400, Deep → blue-400, REM → purple-400, Awake → slate-400.
- **Type:** system sans, `font-mono` only for the elapsed timer.
- **Cards:** `bg-white/5` with `border-white/10` and `backdrop-blur-lg`.
- **Mode:** dark only. The native app should follow system dark mode but assets are designed dark-first.

Native equivalents: use SwiftUI semantic colors (`.indigo`, `.pink`, `.green`, `.teal`) plus a custom asset catalog for the cockpit-blue background.

---

## Non-goals

- No Android port.
- No cloud-side diagnosis or apnea scoring. The app is wellness-only.
- No real-time streaming of audio off-device. Clips stay local until the user explicitly exports.
- No social / sharing features.
- No subscription gating in v1.

---

## Open questions

- **Background audio entitlement**: confirm we can run the engine with screen locked under Apple's review guidelines (precedent: AutoSleep, SleepCycle).
- **HealthKit sample type**: write snore duration as a custom `HKQuantitySample` with metadata, or wrap each session as an `HKWorkout`? Decision affects how third-party apps see the data.
- **On-device retention**: default cap (e.g., 30 days) plus a user-facing slider in Settings.
- **Calibration**: the prototype "dB" is FFT-byte-magnitude, not SPL. The native app should either (a) keep the same uncalibrated proxy and label it "intensity" or (b) calibrate against a reference tone. Recommend (a) for v1.
