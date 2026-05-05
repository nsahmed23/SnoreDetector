# SnoreGuard backend

Go services backing the SnoreGuard iOS app. Single Go module, three
binaries (`cmd/<service>`). Phase 4 of the
[port plan](../docs/NATIVE_IOS_PORT_PLAN.md) lands `sync-service`;
phase 5 adds `analytics-service`, `export-service`, and OpenTelemetry
across all three.

| Service | Port | Purpose |
|---|---|---|
| `sync-service` | `:8080` | Apple Sign-In → session JWT; bidirectional event sync |
| `analytics-service` | `:8081` | Per-user daily summaries + all-time totals (read-only) |
| `export-service` | `:8082` | CSV / JSON history export for backup or GDPR portability (read-only) |

## Layout

```
backend/
├── go.mod                           single module, three cmds
├── docker-compose.yml               postgres + services + otel-collector profiles
├── otel-collector-config.yaml       local dev OTel pipeline (logs to stdout)
├── Makefile                         build/vet/test/test-integration/run-{sync,analytics,export}
├── cmd/
│   ├── sync-service/main.go
│   ├── analytics-service/main.go
│   └── export-service/main.go
├── internal/
│   ├── apple/                       Sign in with Apple identity-token verification
│   ├── auth/                        shared bearer-token middleware + UserIDFrom
│   ├── jwt/                         HS256 session-token issue/verify
│   ├── store/                       pgx-backed Postgres (events, users, aggregations)
│   ├── server/                      sync-service-specific handlers + router
│   ├── analytics/                   analytics-service handlers
│   ├── export/                      export-service handlers (streaming CSV/JSON)
│   ├── httpkit/                     tiny shared JSON helpers
│   ├── otel/                        OpenTelemetry setup, env-driven, no-op fallback
│   └── config/                      env-driven configs for each service
├── migrations/                      *.up.sql + embedded runner
├── sqlc/                            schema.sql + queries.sql for codegen
├── deploy/Dockerfile                distroless final image (sync-service)
├── analytics-service/Dockerfile
└── export-service/Dockerfile
```

## Quick start

```bash
# Postgres only
docker compose up -d postgres

# Run any service against it
make run-sync           # :8080
make run-analytics      # :8081
make run-export         # :8082

# Or bring up all three + the OTel collector
docker compose --profile app --profile otel up -d
```

## Endpoints

### sync-service (`:8080`)

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET`  | `/healthz` | — | Liveness + Postgres ping |
| `POST` | `/auth/apple` | — | Verify Apple `identity_token`, upsert user, return session JWT |
| `POST` | `/events` | Bearer | Bulk-upload up to 500 events (idempotent on `client_event_id`) |
| `GET`  | `/events?since=...&limit=...` | Bearer | List own events, ordered by `received_at` ASC |

### analytics-service (`:8081`)

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET` | `/healthz` | — | Liveness + Postgres ping |
| `GET` | `/analytics/summary?start=&end=&tz=` | Bearer | Per-day rollup (count, total ms, avg dB, longest) bucketed in `tz` (default UTC). `start`/`end` are RFC3339; range capped at 366 days. |
| `GET` | `/analytics/totals?since=` | Bearer | All-time-or-since totals plus `first_event_at` / `last_event_at`. |

### export-service (`:8082`)

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET` | `/healthz` | — | Liveness + Postgres ping |
| `GET` | `/export/events.csv?since=&max=` | Bearer | Streaming CSV download. `max` caps total rows (1..1,000,000). |
| `GET` | `/export/events.json?since=&max=` | Bearer | Streaming JSON download (`{"events":[…],"count":N}`). |

Both export endpoints stream rows in 500-event pages, so even
multi-month exports stay flat in memory. Exports filter strictly to
the authenticated user; cross-user reads are tested in
`internal/export/handlers_test.go::TestExportCSV_OnlyOwnEvents`.

## Tests

| Command | Coverage | Needs |
|---|---|---|
| `make test` | All 62 unit + handler tests across `apple`, `auth`, `jwt`, `server`, `analytics`, `export` | Just `go` |
| `make test-integration` | Real Postgres via `pgx` — upserts, idempotent inserts, listing, migrations | Postgres reachable via `DATABASE_URL` |

Integration tests are gated behind `//go:build integration` so they
never run on the default test pass.

## Endpoint detail

All responses are `application/json`. Errors look like
`{"error": "<message>"}`.

### `GET /healthz`
Returns `200` with `{status: "ok", time: "..."}` when Postgres is
reachable, `503` with `db_error` when it isn't.

### `POST /auth/apple`
Body: `{"identity_token": "<JWT from ASAuthorizationAppleIDCredential>"}`.
On success, upserts the user and returns:

```json
{
  "user_id": "9e3a…-uuid",
  "access_token": "eyJ…",
  "access_token_expires_at": "2026-05-06T13:00:00Z",
  "refresh_token": "eyJ…",
  "refresh_token_expires_at": "2026-07-05T12:00:00Z"
}
```

The access token must be sent on subsequent requests as
`Authorization: Bearer <access_token>`.

### `POST /events`
Bulk-uploads up to 500 snore events for the authenticated user.
Re-posting the same `client_event_id` is a no-op (idempotent).

```json
{
  "events": [
    {
      "client_event_id": "device-uuid-1",
      "started_at": "2026-05-05T03:14:00Z",
      "duration_ms": 1500,
      "avg_db": 62.5,
      "session_id": "rec-2026-05-05"
    }
  ]
}
```

Response: `{"inserted": 1, "received": 1}`.

`avg_db` must be a finite number in `[0, 200]` — `NaN`, `±Inf`, and
out-of-range values are rejected.

**All-or-nothing batches**: if any event in the batch fails validation,
the entire request returns `400` with no inserts. This is intentional —
clients get atomic-import semantics so a partial-success ambiguity
never arises.

### `GET /events?cursor=<opaque>&limit=<n>`
Returns events ordered ascending. `limit` is clamped to `[1, 1000]`
(default 200). The response includes `next_cursor` when more pages are
available; pass it back as `cursor=` for the next page. The cursor is
opaque (base64) — clients must not parse it.

For backward compatibility, `?since=<RFC3339>` is also accepted; it is
treated as `cursor=(since, nil-uuid)`. New clients should use `cursor`.

### `POST /auth/refresh`
Body: `{"refresh_token": "..."}`. Returns the same shape as
`/auth/apple`. The refresh token is rotated on every use — the old
token is marked `replaced_by` and rejected on subsequent calls.

### `POST /auth/logout`
Body: `{"refresh_token": "..."}`, with the access token in
`Authorization: Bearer …`. Revokes the access token's `jti` and the
refresh token's entire family.

### `GET /export/events.csv` and `GET /export/events.json`

Both stream the authenticated user's events ordered by `received_at` ASC,
paginated internally in 500-event pages so memory stays flat. Optional
`?since=<RFC3339>` and `?max=<n>` (1..1,000,000).

**Partial-response on store error (MVP behavior).** If Postgres errors
mid-stream after the response body has begun, the response is
**HTTP 200 with a truncated body**:

- `events.csv`: a row may be missing or the stream ends without a
  trailing newline. Clients should detect "fewer rows than the
  server's row count would imply" via the existing reconciliation
  flow (no header row gives the count, but a short export is the
  signal).
- `events.json`: the body is missing the closing `]}` and the `count`
  field. Treat malformed trailing JSON as "this export was partial,
  retry later."

We accept this trade for MVP: switching to chunked-encoding error
trailers or NDJSON for per-row error localizability is future work.

## Refresh tokens

- **Access TTL**: 1 hour. Stateless HS256 JWT.
- **Refresh TTL**: 60 days. Backed by the `refresh_tokens` table so
  rotation and revocation are persistent.
- **Rotation**: every `/auth/refresh` issues a new refresh token and
  marks the previous one's `replaced_by` column. Reusing a previously
  rotated refresh token is treated as token theft — the **entire family
  is revoked** (every refresh token sharing the same `family_id`).
  This bounds blast radius if a refresh token leaks.
- **Logout**: revokes both the access JTI (consulted by middleware via
  `revoked_jti`) and the refresh family.
- **Storage**: `refresh_tokens (token_id, user_id, family_id,
  issued_at, expires_at, revoked_at, replaced_by)` and
  `revoked_jti (jti, user_id, revoked_at)`.

## Pagination

`GET /events` uses a **compound cursor** of `(received_at, id)`. The
old single-timestamp cursor silently dropped rows when more than `limit`
events shared the same `received_at` (which is exactly what happens
when an iOS client uploads a batch — every row gets `NOW()` from the
same transaction). The new query is:

```sql
WHERE user_id = $1 AND (received_at, id) > ($2, $3)
ORDER BY received_at ASC, id ASC
LIMIT $4
```

The cursor is `base64url(rfc3339nano + "|" + uuid)`. The first page
sends the zero cursor (`(epoch, nil-uuid)`), which the row-tuple
comparison treats as "everything". Migration `004` adds the matching
`(user_id, received_at, id)` index.

The same compound cursor is threaded through the **export streaming**
loops in `internal/export/handlers.go`, so multi-row batches with a
shared `received_at` no longer silently truncate the export.

## Configuration (env-driven)

Every service reads:

| Variable | Default | Purpose |
|---|---|---|
| `ADDR` | `:8080` (sync) / `:8081` (analytics) / `:8082` (export) | Listen address |
| `DATABASE_URL` | *(required)* | Postgres connection URL |
| `JWT_SIGNING_KEY` | *(required, ≥32 bytes)* | HS256 key shared across all three services. Sync issues access tokens; analytics + export verify them. |
| `JWT_ISSUER` | `snoreguard-sync` | Access-token `iss` |
| `JWT_TTL` | `1h` | Access-token lifetime (only sync issues; analytics + export only verify) |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
| `REQUEST_TIMEOUT` | `15s` (sync, analytics) / `60s` (export) | Per-request timeout |

`sync-service` additionally requires:

| Variable | Default | Purpose |
|---|---|---|
| `APPLE_AUDIENCE` | `com.snoreguard.app` | iOS bundle ID — must match the app's |
| `APPLE_ISSUER` | `https://appleid.apple.com` | Apple JWT `iss` |
| `JWT_REFRESH_ISSUER` | `snoreguard-refresh` | Refresh-token `iss` (must differ from `JWT_ISSUER`) |
| `JWT_REFRESH_TTL` | `1440h` | Refresh-token lifetime (60d) |

### OpenTelemetry

All three services follow the standard OTel env-var spec:

| Variable | Effect |
|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP endpoint, e.g. `http://otel-collector:4318`. **Empty disables export** (no-op providers, otelhttp instrumentation still works.) |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` (default) or `grpc` |
| `OTEL_SERVICE_NAME` | Override the service name in resource attributes |
| `OTEL_RESOURCE_ATTRIBUTES` | Extra attributes (`key1=val1,key2=val2`) |

The local docker-compose ships an `otel-collector` profile that
prints traces + metrics to stdout. Swap the exporter in
`otel-collector-config.yaml` for production.

## Migrations

`migrations/*.up.sql` files are `embed.FS`-bundled into the sync-service
binary and applied on startup by `migrations.Up`. Each migration is
recorded in `schema_migrations(version, checksum)`; if a previously-
applied migration's checksum changes, startup aborts rather than
silently re-applying.

## Threat model

- **Algorithm-confusion**: Apple verifier whitelists `RS256`/`ES256`;
  session-token verifier whitelists `HS256`. `alg=none` and HS256-as-
  RS256 forging are both rejected.
- **Cross-user reads**: every read filters by the JWT-derived `user_id`.
  Tested across sync, analytics, and export.
- **Refresh-token theft**: rotation + reuse-detection. Presenting a
  previously rotated refresh token revokes the entire `family_id` so
  blast radius from a leaked refresh token is bounded.
- **Access-token revocation**: logout records the `jti` in `revoked_jti`;
  every authenticated request (sync, analytics, export) consults that
  table via the shared `auth.Middleware`.
- **Email overwrite**: Apple returns `email` only on first sign-in; the
  store uses `COALESCE(EXCLUDED.email, users.email)` so re-sign-in
  doesn't blank the stored email.
- **Logging hygiene**: handlers log error messages but never raw tokens
  or PII bodies.

## sqlc

Hand-written `pgx` code in `internal/store/store.go` is the source of
truth today. The `sqlc/` directory holds the equivalent `schema.sql` +
`queries.sql` so that, when the schema grows, you can regenerate
typed query code via `make sqlc`. The two paths must stay in sync.
