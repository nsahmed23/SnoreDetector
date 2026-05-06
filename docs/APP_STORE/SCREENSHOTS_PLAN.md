# App Store Connect — Screenshots plan

Apple requires screenshots at three sizes for v1 (universal app):

| Device class | Required size | Recommended source device |
| --- | --- | --- |
| 6.7" iPhone (Pro Max) | 1290 × 2796 | iPhone 15 Pro Max simulator |
| 6.5" iPhone (XS Max class — *grandfathered*) | 1284 × 2778 OR 1242 × 2688 | reuse 6.7" via Xcode resize |
| 5.5" iPhone (8 Plus class — required by Apple for older device support) | 1242 × 2208 | iPhone SE (2nd gen) simulator |

Screen counts: 3 minimum, 10 maximum per locale.

## What each screenshot should show

Plan: **7 shots** (optionally 8) per size for v1. Same composition across the three device classes.

### Shot 1 — Record tab, idle
- Caption: "Listens. Doesn't record."
- Composition: bottom 60% of the screen, Record tab. Big "Start listening" button visible. Disclaimer banner at top in the screenshot frame.
- Why: anchors the disclaimer-first message before users see anything that looks medical.

### Shot 2 — Record tab, mid-session
- Caption: "Heuristic detector — not a medical device."
- Composition: live duration `01:34`, event count `7 snore-like events`, level meter showing the bar above the threshold marker.
- Why: shows the live UX without claiming diagnostic value.

### Shot 3 — Settings: detection (threshold + sensitivity)
- Caption: "Tune to your room."
- Composition: threshold slider at 60 dB, sensitivity `Medium`. Detection-only section visible.
- Why: shows configurability without overpromising.

### Shot 4 — Settings: Apple Health (NEW)
- Caption: "Apple Health — opt in to what you actually want."
- Composition: the three independent HealthKit toggles, each with its own subtext:
  - "Read sleep stages" — off by default, subtext "Correlate detected events with your sleep stages on-device. Never uploaded."
  - "Write SnoreGuard sessions" — off by default, subtext "Saved to Apple Health as inBed samples. Estimated, not medical."
  - "Write estimated sound levels" — off by default, subtext "Uncalibrated, not diagnostic. Per-event audio-exposure samples."
- Why: makes the three-direction split visually obvious; the inBed promise is on-screen.

### Shot 5 — Settings: Cloud sync (NEW)
- Caption: "Your audio. Your account. Your call."
- Composition: three toggles in a Cloud sync section:
  - "Cloud sync" — off by default
  - "Upload audio clips" — off by default, only enabled when Cloud sync is on; subtext "Short clips around detected events upload to your account. Delete any clip from History at any time."
  - "Wi-Fi only" — on by default when Cloud sync is on; subtext "Don't burn cellular data on uploads."
- Why: shows the double opt-in for clip upload and the explicit Wi-Fi-only default.

### Shot 6 — Settings: About + privacy link
- Caption: "Open-source. On-device. No tracking."
- Composition: version + build + "Read the disclaimer in full" link + "Privacy & data lifecycle" link. Footer "Not a medical device" text.
- Why: trust signals.

### Shot 7 — History tab with sessions list + cloud-sync badges (NEW, post-Phase-2-C)
- Caption: "See last night, the night before, the week."
- Composition: list of recent sessions with start time, event count, and a small cloud-sync badge per session (synced / local-only). At least one session shows a clip-availability indicator.
- Why: shows the persistence story and that cloud-sync state is transparent per-session.

### (Optional) Shot 8 — Per-session detail
- Caption: "Every event is a row. Every clip is yours to delete."
- Composition: per-session detail showing event timestamps + clip availability indicator + the per-clip delete affordance.
- Why: closes the loop on the deletion-control story. Skip if we want to stay at 7 shots.

## Capture procedure (Mac-only)

1. Boot a clean iPhone 15 Pro Max simulator with `iOS 17.5` runtime.
2. `xcrun simctl boot "iPhone 15 Pro Max"`
3. Run the app: `xcodebuild build && xcrun simctl install booted ./SnoreGuard.app && xcrun simctl launch booted com.snoreguard.app`
4. Use the in-app recording flow with a known audio source (record a 90-second sample with predictable snore-like input — e.g. a 120 Hz tone at -6 dB) so the live numbers in the screenshot are reproducible across attempts.
5. `xcrun simctl io booted screenshot ~/Desktop/snoreguard-shot-N.png`
6. Repeat at 6.5" by switching simulator device, OR resize the 6.7" PNG to 1284×2778 in Preview (Apple accepts upscaled lower-res for grandfathered sizes).
7. Repeat at 5.5" with iPhone SE simulator (must capture natively — no upscale-of-larger).

## Localization

v1 ships en-US only. Once we have translations:
- Reshoot at each locale (Apple requires localized screenshots if the app is localized).
- Caption strings live in `assets/screenshots/<locale>/captions.json` — keep them out of the binary.

## Asset paths

Once captured, store at:
```
assets/screenshots/
├── en-US/
│   ├── 6.7/
│   │   ├── shot-1.png
│   │   ├── shot-2.png
│   │   ├── shot-3.png
│   │   ├── shot-4.png
│   │   ├── shot-5.png
│   │   ├── shot-6.png
│   │   └── shot-7.png
│   ├── 6.5/
│   │   └── ...
│   └── 5.5/
│       └── ...
```

(This phase doesn't ship screenshots — capture happens on the first Mac session. The directory structure is documented here so the asset uploader knows where to find them.)
