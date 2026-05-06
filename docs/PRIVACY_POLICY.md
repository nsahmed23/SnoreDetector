# SnoreGuard privacy policy

User-facing. Mirrors what we put on the public privacy-policy URL
linked from the App Store listing and from the in-app **Settings →
About → Privacy** screen. Engineering detail (storage layouts, key
material, retention enforcement points) lives in
`PRIVACY_AND_DATA_LIFECYCLE.md`.

**Effective date:** `<TBD before submission>`

---

## What data we collect

We try to collect as little as possible. The full list:

- **Apple `sub`** — the stable, opaque per-app user identifier Apple
  issues on Sign in with Apple. We use it as our `user_id`.
- **Email** — only if you choose to share your real email through
  Apple's flow (you can also share Apple's relay address). Used for
  account recovery and account-deletion correspondence.
- **Session metadata** — start time, end time, device name (e.g.
  "iPhone 15 Pro"), app version. One row per recorded sleep session.
- **Event metadata** — for each detected snore event in a session: a
  timestamp, a duration, and an average noise level (in
  uncalibrated dB derived from the FFT byte magnitudes — *not* a
  reference-grade sound-pressure-level reading).
- **Audio clips** — *only* when you explicitly enable both **Cloud
  sync** and **Upload audio clips**. Short audio segments (a few
  seconds) captured around individual detected events. Intended to
  let you review what triggered a detection.

We do not collect: contacts, location, photos, calendar, Bluetooth
device list, advertising identifiers.

## What we do with it

- Provide cross-device sync and a history view across devices you've
  signed in on.
- Aggregate anonymized telemetry for service health (request counts,
  latencies, error rates). Telemetry never includes per-user
  identifiers we use to look up your account.

## What we never do

- Sell your data.
- Share it with third parties for advertising or marketing.
- Write inferred sleep stages (REM, core, deep) to Apple Health from
  microphone data. The microphone signal cannot support that
  inference; we will not pretend otherwise.
- Retain raw audio in backend logs. Logs strip request bodies on
  ingest endpoints that handle audio.

## Where data is stored

- **On your device:** Core Data + the app-sandbox file system.
  Encrypted at rest by iOS (Data Protection class C — accessible
  while unlocked).
- **In SnoreGuard's cloud** (only if you enable Cloud sync):
  Postgres for metadata, an object store for audio clips. Both behind
  TLS in transit.
- **Never** on third-party advertising networks, analytics SaaS that
  joins data across apps, or marketing CRMs.

## How long we keep it

Defaults — you can change these in **Settings → Storage**:

| Where | Default retention |
|---|---|
| Audio clips on your device | 14 days |
| Audio clips in our cloud (when uploaded) | 30 days |
| Session + event metadata on your device | indefinitely (until you delete) |
| Session + event metadata in our cloud (when synced) | indefinitely (until you delete your account) |

Backups follow whatever iCloud / device-backup policy you've configured
— that's outside the app's control.

## How to delete it

- **Per clip:** swipe-to-delete on any row in the **History** tab.
  Deletes the local copy immediately and queues a backend delete on
  next sync.
- **Per session:** open the session detail view → **Delete session**.
  Removes the session and all its events + clips, locally and (on next
  sync) on the backend.
- **Account-wide:** email <support@example.com> from the address tied
  to your account, or use **Settings → Account → Delete account** when
  the in-app flow ships. We respond within 30 days; on completion, all
  rows associated with your `user_id` are removed from Postgres and
  all clips removed from object storage.
- **Sign out:** removes session + refresh tokens from this device. It
  does **not** delete your server-side data; for that, use the
  account-deletion flow above.

## HealthKit promise

If you enable any of the HealthKit features:

- The three toggles (read sleep, write inBed sessions, write
  uncalibrated audio-exposure samples) are independent. Granting one
  does not grant the others.
- We never write inferred sleep stages from microphone data.
- We never read HealthKit categories outside the three the app
  declares. iOS would block us if we tried.
- Revoke access at any time in **Settings → Health → Data Access &
  Devices → SnoreGuard** on your device. Revocation is immediate and
  takes effect without reinstalling the app.

## Your rights

- **Export.** Tap **Settings → Export** in the app. We email a CSV or
  JSON file containing all events, sessions, and clip metadata
  associated with your account.
- **Delete.** Per the section above.
- **Correct.** If anything in your exported data looks wrong, email
  <support@example.com> and we'll investigate.
- **Withdraw consent.** Toggling Cloud sync off stops further uploads
  from this device immediately. To remove what's already on the
  backend, use the delete flow.

## Contact

Questions about this policy: <support@example.com>.

## Changes to this policy

We update this policy in version-controlled files in the public
repository. The **Effective date** above changes whenever the policy
materially changes. Material changes are also surfaced in the app via
a one-time disclosure card on the next launch after the update.

## See also

- [`PRIVACY_AND_DATA_LIFECYCLE.md`](./PRIVACY_AND_DATA_LIFECYCLE.md) —
  engineering detail: where each field is stored, how retention is
  enforced, what the deletion path looks like end-to-end.
- [`DISCLAIMER.md`](./DISCLAIMER.md) — the not-a-medical-device
  notice. The app is a wellness prototype; nothing it shows you is
  medical advice.
