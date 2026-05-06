# SnoreGuard threat model

This document is a living threat model for the SnoreGuard iOS app + Go
backend. It identifies assets, actors, trust boundaries, and the
specific threat classes we mitigate today; it also enumerates open
risks we have not yet closed.

---

## Assets

| Asset | Sensitivity | Where it lives |
|---|---|---|
| Apple `sub` (user identity) | Medium | iOS Keychain (transient), backend Postgres `users` |
| User email (only if user shares on first sign-in) | Medium | Backend Postgres `users` |
| Access tokens (HS256 JWT) | High | iOS Keychain (`AfterFirstUnlockThisDeviceOnly`) |
| Refresh tokens (HS256 JWT, rotated) | High | iOS Keychain + backend Postgres `refresh_tokens` |
| Snore-event metadata (start, duration, avg dB-ish) | Medium | iOS Core Data + backend Postgres `events` |
| Recording-session metadata | Medium | iOS Core Data + backend Postgres |
| Raw audio clips around detected events | **High** | iOS app container; (opt-in) backend object store |
| Sleep-stage reads from HealthKit | High | Apple Health (system-mediated) — never uploaded |
| Backend Postgres database | High | Managed Postgres in prod |
| Object-store contents | High | Filesystem (dev) / GCS or S3 (prod) |
| Telemetry (traces + metrics + logs) | Medium | OTel collector → vendor / Tempo / Mimir |

---

## Actors

| Actor | Capability | Stance |
|---|---|---|
| Legitimate user | Owns the device, the Apple account, and any data tied to it | Trusted within their own scope |
| Device thief | Has physical access; may attempt to extract Keychain or app data | Adversary |
| Network attacker (passive) | Sees traffic between device and backend | Adversary |
| Network attacker (active) | Can MITM, replay, or modify traffic | Adversary |
| Backend operator (insider) | Has Postgres + object-store credentials | Trusted but auditable |
| Supply-chain attacker | Can publish a malicious crate / module / image | Adversary |
| App Store reviewer | Inspects the app for policy compliance | Benign |
| Audited regulator | Inspects privacy posture for compliance | Benign |

---

## Trust boundaries

1. **Device → backend** — TLS over HTTPS. Bearer JWT in `Authorization`.
2. **Backend → object store** — authenticated SDK access; UUID-keyed
   objects; per-clip sha256 verification at upload time.
3. **Backend → Postgres** — network boundary, ideally inside a private
   VPC in production. Connection-pooled `pgx`. Parameterized queries.
4. **iOS app ↔ HealthKit** — system-mediated. The app never bypasses
   `HKHealthStore` authorization checks.

---

## Auth + session threats

| Threat | Vector | Mitigation |
|---|---|---|
| Stolen access token | Device compromise; on-wire capture | Short TTL (1h); `revoked_jti` table consulted by `auth.Middleware` on every authenticated request; algorithm allowlist (`HS256` only) |
| Stolen refresh token | Device compromise; backend storage leak | Rotation on every use; reuse of a previously-rotated token revokes the entire `family_id` (theft response); 60d TTL; stored in Keychain on device |
| Apple identity-token replay | Captured first-sign-in token | Apple verifier checks `iss` / `aud` / `exp`; algorithm allowlist (`RS256` / `ES256`); audience must match `APPLE_AUDIENCE`; tokens are short-lived from Apple |
| Algorithm-confusion attack | Adversary submits HS256-signed token with `alg=RS256` claim | Apple verifier explicitly whitelists `RS256` / `ES256`; session-token verifier explicitly whitelists `HS256`; `alg=none` is rejected |
| Cross-user reads | Adversary with valid token tries to read another user's events | Every store query filters by `UserIDFrom(ctx)`; tested in `internal/server`, `internal/analytics`, `internal/export` |
| Session fixation | Adversary plants a known refresh token | Refresh tokens are server-generated random secrets, not user-controlled; rotation invalidates any planted value on first use |
| Email overwrite | User re-signs-in and Apple omits email | Store uses `COALESCE(EXCLUDED.email, users.email)` on upsert |

### Token theft / replay matrix

| Token | Lifetime | Storage | Replay defense |
|---|---|---|---|
| Apple identity token | Apple-controlled (~10min) | Memory only on device | `iss`/`aud`/`exp` check + algorithm allowlist; one-shot exchange for SnoreGuard tokens |
| SnoreGuard access token (HS256) | 1h | Keychain (device); never in storage server-side | `revoked_jti` lookup on every request; short TTL bounds replay window |
| SnoreGuard refresh token | 60d | Keychain (device) + `refresh_tokens` (backend) | Rotation on use + family revocation on reuse |
| OTel/log sink credentials | Operator-managed | Env / secret manager | Out of band; never on the request path |

---

## Raw audio sensitivity

Raw audio clips are the highest-impact data class in the system: they
are recordings from inside a user's bedroom.

Mitigations:

- **Double opt-in**: cloud sync ON + audio upload ON. Both default off.
- **Clip-around-event semantics**: only short clips around detected
  events are uploaded. Continuous all-night raw audio is never
  uploaded.
- **sha256 verification**: the client computes sha256 of the clip; the
  server recomputes it on receipt and rejects on mismatch.
- **Opaque UUID keys**: object-store keys are random UUIDs, not paths
  derived from user data. Listing the bucket reveals no user-meaningful
  information.
- **Soft-delete + retention**: per-clip `DELETE /audio/clips/{id}` marks
  the metadata row deleted and removes the underlying object;
  retention default (operator-configurable) prunes server-side clips
  after a window.
- **No raw audio in observability**: spans, metrics, logs never include
  audio bytes, clip filenames, or anything derived from clip content.
  The span attribute exposing the clip carries the clip ID and byte
  length only.

---

## HealthKit sensitivity

Writing to a user's Apple Health record is high-trust.

Mitigations:

- Three independent opt-in toggles (per ADR 0004).
- Each toggle drives a separate `requestAuthorization` call.
- Sound-level samples carry uncalibrated metadata
  (`SnoreGuard.Source = "uncalibrated_microphone"`).
- A product-level rule: **never infer REM / core / deep sleep stages
  from the microphone signal**. The detector does not output sleep
  stages, period.
- The Settings UI reflects real `HKHealthStore.authorizationStatus(for:)`,
  not just a local boolean.

---

## Object-storage risks

| Risk | Mitigation |
|---|---|
| Unintended public access on the bucket | Operator must configure bucket as private (deploy-target-dependent); production runbook documents the required ACL/IAM policy |
| Key collisions | UUID v4 keys; `Put` checks `Stat` and refuses to overwrite an existing key |
| Orphan objects after partial-failure delete | Soft-delete metadata first, then delete object; reconciliation job (future) sweeps orphans by listing the bucket and cross-checking against the metadata table |
| Misrouted upload (one user's clip ends up under another's metadata) | Clip-metadata row is created server-side from the JWT-derived `user_id`; the client cannot specify the owner |
| Unauthenticated download of a known clip ID | Download is proxied through the auth-protected handler; the handler verifies the clip belongs to the requesting user before fetching from the store |

---

## API abuse

- **Rate limits**: per-IP and per-user-id rate limiting on every
  authenticated endpoint. Abuse is rejected with `429 Too Many Requests`.
- **Export budget**: per-user-per-24h byte budget on the export
  endpoints, so a malicious or compromised account can't pull
  unbounded history.
- **Request size caps**: bulk event upload is capped at 500 events per
  call; multipart uploads are capped at a configured byte limit.
- **All-or-nothing batches**: bulk inserts either fully succeed or
  fully fail validation, eliminating ambiguous partial states.

---

## Export abuse

Export is read-only of the user's own data, so the threat is
amplification (one compromised account → unbounded data egress).
Mitigations: rate limit + 24h budget. Future TODO: signed download URLs
for clip bytes so the export stream itself is decoupled from the
backend's network egress.

---

## Telemetry leakage

Explicitly **never** present in spans, metrics, or logs:

- Raw user IDs (UUIDs, Apple `sub`).
- Email addresses.
- Access or refresh tokens.
- Audio bytes or any derivative (waveforms, spectrograms).
- Clip filenames.
- Request bodies that contain any of the above.

What is allowed: hashed user IDs (`metrics.HashUserID(uid)` →
`sha256(uid)[:8]` hex), HTTP route, status code, handler name, counts,
durations, query names.

---

## Mobile device compromise

- Tokens stored in iOS Keychain with
  `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`. They do not
  migrate to a new device via iCloud Keychain restore.
- Biometric lock for sensitive operations (clip review, export)
  **TBD** — open work.
- App-data exclusion from iCloud backup for the local clip store
  **TBD** — open work; setting `URLResourceKey.isExcludedFromBackupKey`
  on the clip directory is the planned approach.

---

## Mitigations summary

| Class | Mitigation |
|---|---|
| Token theft | Short access TTL + revoked_jti + refresh rotation + family revocation |
| Algorithm confusion | Verifier algorithm allowlists (`RS256`/`ES256` for Apple, `HS256` for session) |
| Cross-user reads | Every store query filters by JWT-derived user id |
| Replay | Apple `iss`/`aud`/`exp` + access TTL + refresh rotation |
| Raw audio leakage | Double opt-in, sha256, UUID keys, soft-delete + retention, no audio in spans/logs |
| HealthKit overreach | Three opt-in toggles; uncalibrated metadata; never-infer-stages rule |
| API abuse | Per-IP + per-user rate limit + 24h export budget |
| PII in telemetry | Hashed user IDs only; redaction rules grep-able by constant name |
| Object-store orphans | Soft-delete metadata before object delete; future reconciliation sweeper |

---

## Open risks

- **Distributed rate limiting** — current rate limiter is single-replica
  in-memory. Multi-replica deploys require a Redis-backed limiter or
  edge-layer enforcement. Tracked.
- **Persistent retry queue** — sync retries on the iOS client are
  in-memory today; killing the app loses a not-yet-retried upload.
  Persistent queue (Core Data backed) is open work.
- **Object-storage encryption-at-rest** — deploy-target-dependent. GCS
  and S3 both offer SSE; the runbook must enforce it. Filesystem
  blobstore in dev is unencrypted; that is intentional for dev
  ergonomics.
- **Biometric lock and iCloud-backup exclusion on clip storage** —
  documented above, both open.
- **Reconciliation sweeper for orphaned object-store keys** — future
  scheduled job.
- **Signed download URLs** — currently we proxy clip downloads
  through the backend. Signed URLs would offload bandwidth but require
  per-backend support (filesystem cannot sign).
