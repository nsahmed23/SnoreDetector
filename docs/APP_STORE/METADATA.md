# App Store Connect — Listing metadata

These are the strings Apple wants per locale at submission time. English (en-US) is the only locale planned for v1; localizations land post-launch.

## App name
SnoreGuard

(Bundle ID: `com.snoreguard.app`. Subtitle: 30-char limit.)

**Subtitle**: `Heuristic snore detection`

## Promotional text (170 chars max — editable post-release)
Track snoring patterns with a frequency-aware heuristic detector. All audio stays on your device. Apple Health sleep correlation. Cross-device history sync.

## Description (4000 chars max)

**SnoreGuard** is a wellness app that listens for snore-like audio while you sleep, using a frequency-aware heuristic detector — not a medical algorithm, not machine learning, not a diagnostic tool. It's a way to get a feel for whether snoring is happening, when, and how often.

**How it works**

A small Rust algorithm runs on your phone, scanning short audio frames for two things: enough volume to cross your threshold, and a low-frequency dominance pattern characteristic of snoring (which lets it ignore broadband noise like fans, audiobooks, or HVAC). When both conditions hold for about a quarter-second, it counts an event. That's the whole detector — no AI, no cloud inference, no audio leaving your phone.

**What you get**

- A live recording view with a level meter, threshold marker, and event counter.
- History of every session: per-night and per-week trends.
- Optional Apple Health integration for correlation with your sleep stages.
- Optional cross-device sync so you can review last night on your iPad.
- CSV / JSON export for personal records or doctor visits.

**What it isn't**

This is **not** a medical device. It does not screen for sleep apnea, UARS, or any other condition. It's a wellness tool. If you're worried about your breathing during sleep, see a clinician — no app substitutes for a real polysomnography.

**Privacy**

- Audio is processed on-device, never recorded or uploaded.
- Sign in with Apple is the only sign-in method.
- The optional sync feature transmits only event metadata (timestamps, duration, average noise level), never audio.
- HealthKit access is opt-in; the calibration disclaimer for written values is in every sample's metadata. Read access (sleep stages) is read-only — we never modify your sleep data.

**Open-source**

The algorithm, the iOS app, and the backend are all available under MIT on GitHub. See the link below.

## Keywords (100 chars total, comma-separated, no spaces)
snore,sleep,sleeptracking,snoring,monitor,sleeptracker,ambient,health,bedroom,record

## Support URL
https://github.com/nsahmed23/SnoreDetector/issues

## Marketing URL
https://github.com/nsahmed23/SnoreDetector

## Privacy policy URL
**[PRODUCT DECISION]** — must be a live URL by submission. Suggested: a hosted markdown rendering of `docs/DISCLAIMER.md` plus the privacy-relevant subset of `PRIVACY_LABELS.md`. Could be GitHub Pages off this repo.

## Category

- Primary: Health & Fitness
- Secondary: Lifestyle

## Age rating

- 4+ (no objectionable content; nothing to disclose under Apple's questionnaire)

## What's New (release notes — per-version)

### v0.1.0 (initial release)

- Heuristic snore detection (FFT energy + low-frequency dominance).
- Live recording with level meter and event count.
- Three tabs: Record, History (placeholder until v0.2), Settings.
- Sign in with Apple + cross-device sync.
- Apple Health sleep-stage read access.
- Disclaimer: heuristic detector, not a medical device.

### Template for future versions

```
v0.x.0
- <user-facing change 1>
- <user-facing change 2>
- Performance & stability fixes.
```

## See also

- `PRIVACY_LABELS.md` for the privacy nutrition label answers.
- `SCREENSHOTS_PLAN.md` for what each screenshot must show.
- `../DISCLAIMER.md` for the in-app legal copy this listing must stay consistent with.
