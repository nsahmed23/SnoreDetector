# SnoreGuard (SnoreDetector)

[![Rust core](https://github.com/nsahmed23/SnoreDetector/actions/workflows/rust.yml/badge.svg)](https://github.com/nsahmed23/SnoreDetector/actions/workflows/rust.yml)
[![Go backend](https://github.com/nsahmed23/SnoreDetector/actions/workflows/go.yml/badge.svg)](https://github.com/nsahmed23/SnoreDetector/actions/workflows/go.yml)
[![Go integration](https://github.com/nsahmed23/SnoreDetector/actions/workflows/integration.yml/badge.svg)](https://github.com/nsahmed23/SnoreDetector/actions/workflows/integration.yml)
[![Web prototype](https://github.com/nsahmed23/SnoreDetector/actions/workflows/web.yml/badge.svg)](https://github.com/nsahmed23/SnoreDetector/actions/workflows/web.yml)
[![iOS app](https://github.com/nsahmed23/SnoreDetector/actions/workflows/ios.yml/badge.svg)](https://github.com/nsahmed23/SnoreDetector/actions/workflows/ios.yml)

A mobile-first snore monitoring app. The current repo contains a polished React
prototype that doubles as the **visual product spec** for the native iOS build.
The real product is a Swift app with a Rust algorithm core and Go backend
services — see [`docs/NATIVE_IOS_PORT_PLAN.md`](docs/NATIVE_IOS_PORT_PLAN.md).

> **Not a medical device.** SnoreGuard is a wellness prototype. It does not
> diagnose sleep apnea or any sleep disorder. See
> [`docs/DISCLAIMER.md`](docs/DISCLAIMER.md).

## Features (prototype)

* **Snore detection** — tracks via a frequency-aware heuristic (FFT energy +
  low-frequency dominance + sustained-frame counter) with adjustable threshold
  and sensitivity. *(Heuristic detector — not ML.)* Designed to ignore
  audiobooks and broadband background noise.
* **HealthKit sync (mocked)** — toggles for reading Sleep Stages and writing
  nightly snoring duration / average intensity. Real `HKHealthStore` integration
  arrives in the native iOS app.
* **Live monitoring** — recording interface with real-time decibel meter,
  threshold marker, ripple visualizer, and live session stats.
* **Analytics** — daily / weekly / monthly trends rendered with `recharts`,
  with snore events overlaid on the sleep-stage chart.
* **Audio playback** — review snore clips grouped by sleep stage (mocked in the
  prototype).
* **CSV export** — last 7 / 30 days or custom range.
* **Onboarding** — three-step intro modal.

## Repository layout

```
SnoreDetector/
├── src/         React prototype (visual product spec)
├── docs/        PRODUCT_SPEC.md, NATIVE_IOS_PORT_PLAN.md, DISCLAIMER.md
├── ios/         Native iOS Swift app (stub — see port plan, phase 2)
├── rust-core/   Rust algorithm core (stub — see port plan, phase 1)
└── backend/     Go services (stub — see port plan, phases 4–5)
```

## Documents

- [`docs/PRODUCT_SPEC.md`](docs/PRODUCT_SPEC.md) — feature-by-feature mapping
  from the web prototype to native iOS equivalents.
- [`docs/NATIVE_IOS_PORT_PLAN.md`](docs/NATIVE_IOS_PORT_PLAN.md) — Swift app
  layout, Rust core build (cargo + cbindgen + xcframework), Swift ↔ Rust C FFI
  boundary, Go backend services, phasing.
- [`docs/DISCLAIMER.md`](docs/DISCLAIMER.md) — privacy and not-a-medical-device
  notice.

## Running the prototype

```bash
npm install
npm run dev
# open http://localhost:3000 — best viewed in a mobile viewport
```

`npm run lint` runs `tsc --noEmit`.

## Tech stack (prototype)

- React 19, TypeScript, Vite 6
- Tailwind CSS v4, Lucide icons
- Recharts for charts
- Web Audio API (`AudioContext` + `AnalyserNode`) for real-time FFT capture

## Branch protocol

- Web prototype changes go to `main` via short-lived branches.
- Native work lands in feature branches scoped to a single phase
  (`rust-core-skeleton`, `ios-record-tab`, `ios-healthkit`, `backend-sync`,
  `backend-analytics-export`).

## iOS context note

The prototype is designed with iOS mechanics in mind: the recording flow
assumes `AVAudioSession` configured with `.playAndRecord` and `.mixWithOthers`,
so the user can keep an audiobook playing on AirPods while the app monitors
ambient audio. The native app is where this actually happens.
