# SnoreGuard backend

Go services that back the SnoreGuard iOS app. Phase 4 of the [native
port plan](../docs/NATIVE_IOS_PORT_PLAN.md) lands the **sync-service**
described here. Phase 5 will add **analytics-service** and
**export-service**.

```
backend/
├── go.mod                           single module, multiple cmds
├── docker-compose.yml               Postgres + service profiles for local dev
├── Makefile                         build, vet, test, test-integration, run-sync
├── cmd/
│   └── sync-service/main.go         entrypoint
├── internal/
│   ├── apple/                       Apple Sign-In identity-token verification
│   ├── jwt/                         session JWT issue/verify (HS256)
│   ├── store/                       pgx-backed Postgres store
│   ├── server/                      chi router, middleware, handlers
│   └── config/                      env-driven config loader
├── migrations/                      *.up.sql + embedded runner
├── sqlc/                            schema.sql + queries.sql for codegen
└── deploy/Dockerfile                distroless final image (sync-service)
```

## Quick start

```bash
# 1. Start Postgres
docker compose up -d postgres

# 2. Run the service
make run-sync

# 3. Smoke test
curl localhost:8080/healthz
```

## Tests

| Command | What it covers | Needs |
|---|---|---|
| `make test` | Apple verification, JWT round-trip, middleware, all handlers (with a fake store) | Just `go` |
| `make test-integration` | Real Postgres via `pgx` — upserts, idempotent inserts, listing, migrations | Postgres reachable via `DATABASE_URL` |

The integration tests are gated behind `//go:build integration` so they
never run on the default test pass.

## API

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

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `ADDR` | `:8080` | Listen address |
| `DATABASE_URL` | *(required)* | Postgres connection URL |
| `APPLE_AUDIENCE` | `com.snoreguard.app` | iOS bundle ID — must match the app's |
| `APPLE_ISSUER` | `https://appleid.apple.com` | JWT `iss` Apple uses |
| `JWT_SIGNING_KEY` | *(required, ≥32 bytes)* | HS256 key for session tokens |
| `JWT_ISSUER` | `snoreguard-sync` | Access token `iss` |
| `JWT_REFRESH_ISSUER` | `snoreguard-refresh` | Refresh token `iss` |
| `JWT_TTL` | `1h` | Access token lifetime |
| `JWT_REFRESH_TTL` | `1440h` | Refresh token lifetime (60d) |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
| `REQUEST_TIMEOUT` | `15s` | Per-request timeout |

## Migrations

`migrations/*.up.sql` files are `embed.FS`-bundled into the binary and
applied on startup by `migrations.Up`. Each migration is recorded in
`schema_migrations(version, checksum)`; if a previously-applied
migration's checksum changes, startup aborts rather than silently
re-applying.

To roll back manually: `psql $DATABASE_URL -f migrations/001_init.down.sql`.

## sqlc

Hand-written `pgx` code in `internal/store/store.go` is the source of
truth today. The `sqlc/` directory holds the equivalent `schema.sql` +
`queries.sql` so that, when the schema grows, you can regenerate
typed query code via `make sqlc`. The two paths must stay in sync.

## Threat model — quick notes

| Threat | Mitigation |
|---|---|
| **Algorithm-confusion** on Apple tokens | Verifier whitelists `RS256`/`ES256` via `jwt.WithValidMethods`; HS256 forgery using the public key is rejected (`apple/verify_test.go::TestVerify_RejectsHS256`). |
| **Replay** of Apple identity tokens | Short `exp` upstream; one-shot — clients must not re-post the same Apple token. |
| **Cross-user reads** | Every read is filtered by the JWT-derived `user_id`. |
| **Refresh-token theft** | Rotation + reuse-detection: presenting a previously rotated refresh token revokes the entire family. |
| **Access-token revocation** | Logout records the `jti` in `revoked_jti`; middleware consults the table on every request. |
| **Email overwrite** | `users.email` is set with `COALESCE(EXCLUDED.email, users.email)` on upsert — Apple's "Hide My Email" pseudo-email never overwrites a previously stored real address. |
| **Logging hygiene** | Auth handler logs `err.Error()` on verification failures but never the raw token. |
