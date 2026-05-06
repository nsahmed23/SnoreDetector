# SnoreGuard API contract

## What this document is

The contract source of truth is
[`openapi/snoreguard.v1.yaml`](../openapi/snoreguard.v1.yaml). This
file is the human-readable cross-reference: it summarizes the same
shape, links every endpoint back to its Go handler, and captures the
operational semantics the OpenAPI doc can't easily express
(idempotency, partial-stream disclosure, the privacy contract).

When the YAML and this document disagree, the YAML wins.

## Endpoint matrix

| Method | Path | Auth | Rate limit | Idempotency key | OpenAPI operationId | Summary |
|---|---|---|---|---|---|---|
| GET    | `/healthz`                       | none   | n/a              | n/a                  | `getHealthz`           | Liveness + Postgres ping (one per service). |
| POST   | `/auth/apple`                    | none   | 10/min per IP    | n/a                  | `postAuthApple`        | Verify Apple identity token, mint tokens. |
| POST   | `/auth/refresh`                  | none   | 30/min per IP    | n/a                  | `postAuthRefresh`      | Rotate refresh token. |
| POST   | `/auth/logout`                   | bearer | 30/min per user  | n/a                  | `postAuthLogout`       | Revoke access JTI + refresh family. |
| POST   | `/events`                        | bearer | 200/min per user | `client_event_id`    | `postEvents`           | Bulk upload (<= 500), all-or-nothing. |
| GET    | `/events`                        | bearer | 200/min per user | n/a                  | `listEvents`           | Paginated, ASC by `(received_at, id)`. |
| POST   | `/sessions`                      | bearer | 60/min per user  | `client_session_id`  | `postSession`          | Open or refresh a recording session. |
| GET    | `/sessions`                      | bearer | 60/min per user  | n/a                  | `listSessions`         | Paginated, DESC by `(started_at, id)`. |
| POST   | `/audio/clips`                   | bearer | 30/min per user  | `client_clip_id`     | `postAudioClip`        | Multipart upload (`metadata` + `file`). |
| GET    | `/audio/clips`                   | bearer | 60/min per user  | n/a                  | `listAudioClips`       | Paginated, DESC by `(started_at, id)`. |
| DELETE | `/audio/clips/{id}`              | bearer | 60/min per user  | n/a                  | `deleteAudioClip`      | Soft-delete + best-effort blob delete. |
| GET    | `/audio/clips/{id}/download`     | bearer | 10/hour per user | n/a                  | `downloadAudioClip`    | Stream raw audio bytes. |
| GET    | `/analytics/summary`             | bearer | 60/min per user  | n/a                  | `getAnalyticsSummary`  | Per-day rollup in `tz`. |
| GET    | `/analytics/totals`              | bearer | 60/min per user  | n/a                  | `getAnalyticsTotals`   | All-time-or-since totals. |
| GET    | `/export/events.csv`             | bearer | 10/hour per user + 24h budget | n/a | `exportEventsCSV`      | Streaming CSV, optional sections. |
| GET    | `/export/events.json`            | bearer | 10/hour per user + 24h budget | n/a | `exportEventsJSON`     | Streaming JSON, optional sections. |

## Authentication

1. **Sign in** — iOS calls `ASAuthorizationAppleIDProvider`, hands the
   `identity_token` to `POST /auth/apple`. Backend verifies with
   Apple's JWKS, upserts a row in `users`, and returns an
   `AuthTokensResponse`.
2. **Use tokens** — every authenticated call carries the access
   token: `Authorization: Bearer <access_token>`. Lifetime ~1h.
3. **Refresh** — when the access token's `access_token_expires_at`
   is near or a 401 lands, call `POST /auth/refresh` with the
   refresh token. The server rotates the refresh token (the old one
   is marked `replaced_by` and rejected on next use). Replay of an
   already-rotated refresh token revokes the entire family
   (theft response).
4. **Retry policy** — on 401, refresh once and retry the original
   request once. Bail out on a second 401.
5. **Logout** — `POST /auth/logout` with the access token in the
   header and the refresh token in the body revokes both the JTI
   and the family.

## Pagination

All list endpoints emit an opaque base64 `next_cursor` when more
rows exist. The cursor encodes a compound key:

- `/events`: `(received_at, id)` ascending.
- `/sessions`, `/audio/clips`: `(started_at, id)` descending.

The cursor is base64-URL-safe of `<rfc3339nano>|<uuid>` but clients
**MUST** treat it as opaque. The backend reserves the right to
change the encoding without bumping the spec major version. Pass
the cursor back unchanged on the next page; omit it on the first
page.

`limit` is clamped to `[1, 1000]` everywhere. Defaults are
route-specific (200 for events, 100 for sessions/clips).

For backward compatibility the events list endpoint still accepts
`?since=<RFC3339>`, treated as `cursor=(since, nil-uuid)`. New
clients should use `cursor`.

## Idempotency

| Endpoint | Key | Behavior on retry |
|---|---|---|
| `POST /events` | `client_event_id` (per row) | Existing rows skipped; `inserted < received` distinguishes new from existing. |
| `POST /sessions` | `client_session_id` | Returns same `session_id` with `created: false`. `ended_at`/`device_name`/`app_version` updated only when the new value is non-null (COALESCE). |
| `POST /audio/clips` | `client_clip_id` | Returns same `clip_id` with `created: false`. The stored object is **not** overwritten. |

Clients SHOULD generate these keys deterministically (UUIDv4 per
physical event/session/clip, then keep the same UUID across retries).
The keys are scoped to `(user_id, client_*_id)`.

## Error envelope

All non-2xx responses use `{"error": "<message>"}`. Status codes:

| Code | Meaning |
|---|---|
| 200 | OK. |
| 204 | No body (currently unused; reserved). |
| 400 | Malformed request, validation failed, sha256 mismatch on multipart, unknown `tz`/`include`. |
| 401 | Missing/invalid bearer, refresh token rejected. |
| 404 | Clip not found, soft-deleted, or body missing on the blobstore. |
| 409 | Reserved (idempotency keys make true conflicts rare). |
| 413 | Audio body exceeds `AUDIO_MAX_CLIP_BYTES`. |
| 415 | Audio MIME (declared or sniffed) not on allowlist. |
| 429 | Rate limit or per-user export budget exceeded. `Retry-After` header always present. |
| 500 | Internal error. |
| 503 | `/healthz` only — Postgres ping failed. |

## Multipart audio uploads

`POST /audio/clips` is `multipart/form-data` with two parts:

- `metadata` — `Content-Type: application/json`, body conforming to
  `ClipMetadataRequest`. Maximum 16 KiB per part.
- `file` — `Content-Type` from the allowlist
  (`audio/m4a`, `audio/mp4`, `audio/wav`, `audio/aac` by default;
  configurable via `AUDIO_ALLOWED_MIME`). Body capped at
  `AUDIO_MAX_CLIP_BYTES` (default 5 MiB).

Server-side validation:

1. Multipart envelope <= clip cap + 64 KiB overhead.
2. Both `metadata` and `file` parts are required; duplicate parts
   return 400. Unknown parts are ignored (forward-compat).
3. Declared part Content-Type AND sniffed Content-Type must both
   be on the allowlist. Demanding both blocks both header-spoofing
   and body-spoofing in one stroke.
4. sha256 of the bytes received must equal `metadata.sha256`.
5. The `object_key` is server-controlled (`clips/<user_id>/<uuid>`).
   Client-supplied keys are ignored.
6. The DB row is upserted before the blobstore write; on a crash
   between the two, the row points at an absent object and the
   download returns 404. Idempotent retry succeeds without
   overwriting the stored bytes.

There are NO signed URLs. Both upload and download stream through
the API.

## Streaming partial-response disclosure

Both `/export/events.csv` and `/export/events.json` write the HTTP
200 response header before they have finished streaming. If the
underlying `ListEvents`/`ListSessions`/`ListAudioClips` query fails
mid-stream, the response is truncated:

- **CSV**: ends abruptly; clients should compare row count to the
  count returned by `GET /analytics/totals` for the same window
  and retry on mismatch.
- **JSON**: ends without the closing `]}` and without the `count`
  field. Clients SHOULD treat malformed trailing JSON as a partial
  export and retry later.

This is a known MVP behavior. Future work: chunked-encoding error
trailer or NDJSON so per-row errors are localizable.

## `?include=sessions,clips` shape change

When the export request omits `include`, the JSON shape is the
original `{"events":[…],"count":N}` and the CSV is a single events
section. When `include=sessions` and/or `include=clips` is set:

- **JSON** becomes
  `{"events":[…],"sessions":[…],"clips":[…],"count":{events:N,sessions:M,clips:K}}`.
  The `sessions` and `clips` arrays are present only for the
  sections requested; the `count` object always includes `events`
  and adds keys for the sections requested.
- **CSV** appends a blank-line-separated section per requested
  include, each preceded by a `# section: <name>` comment line and
  its own header row.

The OpenAPI spec models the JSON shape change with a `oneOf` between
`ExportJSONBare` and `ExportJSONExtended`. We chose `oneOf` rather
than splitting into two operations because:

- Both shapes hit the same path with the same query handler.
- `oneOf` keeps the operationId stable, so the iOS client can
  switch shapes by toggling a query param without code-gen churn.
- Backward compat is automatic: existing clients that omit
  `include` keep the bare shape forever.

## Privacy contract

The server promises:

- **No raw audio in logs or spans.** Telemetry carries only
  hashed user IDs, clip UUIDs, and byte counts.
- **No user-controlled object keys.** The blobstore key is always
  `clips/<user_id>/<uuid>` — server-assigned, not client-supplied.
- **Soft-delete preserves audit trail.** `DELETE /audio/clips/{id}`
  sets `deleted_at` on the row before attempting the object delete,
  so a later object-delete failure leaves an orphan but the user's
  intent is durable.
- **Strict per-user filtering.** Cross-user reads are tested in
  `internal/export/handlers_test.go::TestExportCSV_OnlyOwnEvents`
  and equivalent suites for `events`, `sessions`, `clips`.
- **No clinical claims.** Surface language stays "uncalibrated
  relative dB". See [`docs/DISCLAIMER.md`](DISCLAIMER.md) for the
  product-side framing the API surface has to respect.

## Source of truth

| Endpoint | Go handler | Swift Endpoint struct |
|---|---|---|
| `GET /healthz` | `backend/internal/server/server.go::healthHandler` | n/a |
| `POST /auth/apple` | `backend/internal/server/auth.go::authHandler.handle` | `AuthAppleEndpoint` |
| `POST /auth/refresh` | `backend/internal/server/auth.go::authHandler.refresh` | (used by `AuthManager`, not as Endpoint struct) |
| `POST /auth/logout` | `backend/internal/server/auth.go::authHandler.logout` | `AuthLogoutEndpoint` |
| `POST /events` | `backend/internal/server/events.go::eventsHandler.create` | `PostEventsEndpoint` |
| `GET /events` | `backend/internal/server/events.go::eventsHandler.list` | `ListEventsEndpoint` |
| `POST /sessions` | `backend/internal/server/sessions.go::sessionsHandler.create` | `PostSessionEndpoint` |
| `GET /sessions` | `backend/internal/server/sessions.go::sessionsHandler.list` | `ListSessionsEndpoint` |
| `POST /audio/clips` | `backend/internal/server/clips.go::clipsHandler.create` | (manual multipart in `AudioClipSync`) |
| `GET /audio/clips` | `backend/internal/server/clips.go::clipsHandler.list` | `ListAudioClipsEndpoint` |
| `DELETE /audio/clips/{id}` | `backend/internal/server/clips.go::clipsHandler.delete` | `DeleteAudioClipEndpoint` |
| `GET /audio/clips/{id}/download` | `backend/internal/server/clips.go::clipsHandler.download` | `DownloadAudioClipEndpoint` |
| `GET /analytics/summary` | `backend/internal/analytics/handlers.go::handler.dailySummary` | (TBD; iOS not yet wired) |
| `GET /analytics/totals` | `backend/internal/analytics/handlers.go::handler.totals` | (TBD; iOS not yet wired) |
| `GET /export/events.csv` | `backend/internal/export/handlers.go::handler.csv` | (TBD; iOS not yet wired) |
| `GET /export/events.json` | `backend/internal/export/handlers.go::handler.json` | (TBD; iOS not yet wired) |

iOS Endpoint structs live in
[`ios/SnoreGuard/Networking/Endpoints.swift`](../ios/SnoreGuard/Networking/Endpoints.swift)
on the `claude/ios-network-client` branch.

## Changelog

| Version | Date | Change |
|---|---|---|
| 1.0.0-draft | 2026-05-06 | Initial spec extracted from PR #15 + #11 source-of-truth handlers. |
