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

A small Rust algorithm runs on your phone. It scans short audio frames for two things: enough volume to cross your threshold, and a low-frequency dominance pattern characteristic of snoring (which lets it ignore broadband noise like fans, audiobooks, or HVAC). When both conditions hold for about a quarter-second, it counts a snore-like event. That's the whole detector — no AI, no cloud inference, no audio leaving your phone unless you explicitly opt in to cloud sync.

**What you get**

- A live recording view with a level meter, threshold marker, and event counter.
- Persistent history of every session: per-night and per-week trends.
- Optional Apple Health integration with three independent toggles: read sleep stages (correlate with detected events on-device), write SnoreGuard sessions as inBed samples, write estimated sound levels as uncalibrated audio-exposure samples. Each toggle defaults off.
- Optional cloud sync via Sign in with Apple — sessions, snore-like events, and (separately opt-in) audio clips around events sync to your account so you can review last night on another device.
- CSV / JSON export of session and event metadata, with optional binary clip download.
- Honest, conservative disclosure copy. SnoreGuard is not a medical device; it doesn't diagnose anything.

**Privacy**

- Audio is processed on-device. Never recorded or uploaded unless you explicitly enable both the Cloud sync and Upload audio clips toggles.
- Sign in with Apple is the only sign-in method. No passwords. No email collection unless you choose to share it through Apple's flow.
- HealthKit access is opt-in per direction (read vs write). Revoke any combination at any time from iOS Settings → Health.
- All values written to Apple Health are flagged as estimated, uncalibrated, and not diagnostic.
- The app never infers REM, core, or deep sleep stages from microphone data.
- Telemetry (when enabled by the operator) excludes raw audio, raw user IDs, and tokens.

**Open source**

The iOS app, the Rust detector core, and the Go backend services are all available on GitHub under MIT.

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
- [../PORTFOLIO_NARRATIVE.md](../PORTFOLIO_NARRATIVE.md) — long-form narrative for the project (links the listing copy back to the engineering story).
- [../DISCLAIMER.md](../DISCLAIMER.md) for the in-app legal copy this listing must stay consistent with.
