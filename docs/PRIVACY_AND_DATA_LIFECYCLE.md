# SnoreGuard privacy and data lifecycle

This document is the user-facing-and-operator-facing source of truth for
what data SnoreGuard collects, where it lives, when it leaves the
device, how long it is retained, and how the user controls each stage.

---

## What's collected

| Data class | Collected by | When |
|---|---|---|
| Apple `sub` (stable per-app user identifier) | iOS app at first sign-in; backend on `/auth/apple` | Sign in with Apple |
| Email (only if user shares on first sign-in) | Forwarded to backend on first `/auth/apple`; never re-asked | Sign in with Apple |
| Device name (e.g., "iPhone 15 Pro") | iOS app; included in session metadata only when cloud sync is on | When a recording session ends |
| Snore-event metadata (start, duration, avg dB-ish, session id) | iOS app from the Rust detector | Each detected event |
| Recording-session metadata (id, start, end, device name) | iOS app | Each recording session |
| Audio clips around detected events | iOS app via `AudioEngine` rolling buffer | Only when a snore-like event is detected; clip duration is a settings parameter |
| Sleep-stage reads (HKCategorySample sleep analysis) | iOS app via `HKHealthStore` | Only if "Read sleep stages" toggle is on |
| Telemetry (traces + metrics + logs) | Backend services | On every request to a backend service |

---

## What stays local (never leaves the device)

- **Raw microphone stream**. The microphone is never streamed to the
  backend. Only short clips around detected events are ever written to
  storage at all, and even those don't leave the device unless the user
  has both cloud sync and audio upload toggles on.
- **Local SQLite / Core Data cache** of sessions, events, and clip
  metadata, used so the app works fully offline.
- **Local audio clip files** in the app container, until the cloud-sync
  pipeline (if enabled) uploads them.

---

## What syncs to the backend

When **cloud sync** is on:

- Recording sessions (id, start, end, device name).
- Snore-like events (id, session id, start, duration, avg dB-ish).

When **cloud sync is on AND audio upload is on**:

- Audio clips around detected events (multipart upload, sha256-verified,
  UUID-keyed).
- Per-clip metadata (id, sha256, byte length, content type).

What is **never** synced to the backend:

- Sleep-stage reads from HealthKit. These stay in Apple Health.
- The continuous microphone stream.
- Anything from the device the user did not explicitly opt in to.

---

## When raw audio uploads happen

A clip is uploaded when **all** of the following are true:

1. The "Cloud sync" toggle is on.
2. The "Upload audio clips" toggle is on.
3. The Rust detector finalized an event and produced a corresponding
   clip in the rolling buffer.

This is an explicit **double opt-in**. Both toggles default off. Audio
upload cannot be turned on without first turning cloud sync on.

The clip is the short window around the detected event, not a continuous
all-night recording. The clip duration is configurable via Settings.

The upload is multipart with a sha256 of the bytes. The backend
recomputes the hash on receipt and rejects on mismatch.

---

## Retention model

| Surface | Default | User control |
|---|---|---|
| Local clips on device | 14 days | Settings → "Keep local clips for…" can shorten |
| Backend clips (when cloud sync + audio upload on) | 30 days | Settings → "Keep cloud clips for…" can shorten; operator policy may shorten further |
| Local event metadata | Indefinite (until user deletes) | Per-event delete in Insights tab |
| Backend event metadata | Indefinite (until user deletes or account is deleted) | Per-event delete via API; account deletion via support |
| Telemetry | Per-vendor (typically 7-30 days) | N/A — no user data in telemetry |

Retention shortening is one-way: shortening the local cap immediately
prunes; shortening the cloud cap requests pruning on the next sync.

---

## Deletion model

- **Per-clip delete from the app**. Removes the local file and (if
  uploaded) calls `DELETE /audio/clips/{id}` which soft-deletes the
  metadata row and removes the underlying object.
- **Per-event delete from the app**. Removes the local event and (if
  synced) calls the corresponding delete endpoint.
- **Account-wide delete**. Available via support contact for v1; a
  self-service "Delete my account" button is a future TODO that will
  cascade-delete the user, all their events, all their sessions, all
  their clip metadata, and (best-effort) all their object-store
  contents.
- **Logout does not delete data**. Logging out clears tokens from
  Keychain. Local data and any cloud-synced data persist.

---

## Export model

- CSV or JSON via `GET /export/events.csv` and `/export/events.json`.
- Streaming, paginated internally in 500-row pages so memory stays
  flat for multi-month exports.
- Output includes per-event metadata and per-clip metadata
  (`clip_id`, `sha256`, `byte_length`).
- Binary clip bytes are downloaded out-of-band via
  `GET /audio/clips/{id}/download` after authenticating.
- Per-user 24-hour export budget bounds export abuse.

---

## HealthKit r/w model

Three independent opt-in toggles in Settings, each driving a separate
`HKHealthStore.requestAuthorization` call.

| Toggle | Scope | What it does |
|---|---|---|
| Read sleep stages | `HKCategoryType(.sleepAnalysis)` read | Lets the Insights tab overlay sleep-stage shading on the timeline |
| Write sessions as `inBed` | `HKCategoryType(.sleepAnalysis)` write — value `.inBed` | Each recording session is written as an `HKCategorySample(.inBed)` |
| Write estimated sound levels | `HKQuantityType.environmentalAudioExposure` write | Per-session average uncalibrated dB-ish value, with metadata flag `SnoreGuard.Source = "uncalibrated_microphone"` |

What we **promise not to do**:

- Never infer REM, core, or deep sleep stages from the microphone
  signal. The detector does not produce sleep stages.
- Never write any HealthKit category or quantity type the user did not
  explicitly enable a toggle for.
- Never read any HealthKit category beyond sleep stages.
- Never upload HealthKit data of any kind to the backend.

The Settings UI reflects real `HKHealthStore.authorizationStatus(for:)`
per toggle, so revoking access from iOS Settings → Health → Data Access
& Devices → SnoreGuard is reflected immediately.

---

## Telemetry exclusions

The following are **never** present in spans, metrics, or logs:

- Raw user IDs (UUIDs, Apple `sub`).
- Email addresses.
- Access tokens, refresh tokens, JWT bodies.
- Audio bytes or anything derived from clip content (waveforms,
  spectrograms).
- Clip filenames.
- HTTP request bodies that contain any of the above.

What is allowed in telemetry:

- Hashed user IDs (`sha256(uid)[:8]`).
- HTTP route templates (`/events`, `/auth/refresh`, etc — not
  user-data-derived paths).
- HTTP status codes.
- Handler names (`sync.events.create`, etc).
- Integer counts (events inserted, bytes uploaded).
- Durations.

See `OBSERVABILITY.md` for the full allow / disallow table.

---

## User controls

| Setting | Default | What it changes |
|---|---|---|
| Sign in with Apple | n/a | Enables backend account; required for cloud sync |
| Cloud sync | Off | Pushes sessions + events to backend; enables a second device to pull |
| Upload audio clips | Off (visible only when cloud sync is on) | Uploads short clips around detected events; double opt-in for raw audio |
| Read sleep stages | Off | Reads sleep stages from Apple Health for the Insights overlay |
| Write sessions as `inBed` | Off | Writes each recording session to Apple Health as `inBed` |
| Write estimated sound levels | Off | Writes per-session uncalibrated sound levels to Apple Health |
| Keep local clips for… | 14 days | Local clip retention window |
| Keep cloud clips for… | 30 days | Backend clip retention window (visible only when cloud sync is on) |
| Detection threshold (dB-ish) | 60 | Per-frame threshold passed into the Rust detector |
| Detector sensitivity | Medium | Low / Medium / High — passed into the Rust detector as a `u8` enum |
| Clip duration | 10 s | Window of audio captured around each detected event |

Every setting persists in `UserDefaults`; HealthKit toggles additionally
reflect the system authorization state.
