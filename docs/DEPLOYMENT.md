# SnoreGuard deployment

This document covers how SnoreGuard's three Go services run in three
environments — **local dev**, **staging**, and **production** — what
secrets they need, how databases migrate, how object storage is wired,
how TLS is handled, and how to roll back when something goes wrong.

The iOS app's deployment story (TestFlight → App Store) is owned by
M7-M8 in `ROADMAP_PRODUCTION_READY.md` and is not covered here.

---

## Local Docker Compose

```bash
cd backend
docker compose --profile app up -d
# postgres + sync-service + analytics-service + export-service
# all three services bind to :8080, :8081, :8082 on the host

# Optional: bring up the OTel collector
docker compose --profile app --profile otel up -d
```

What the compose file boots:

| Service | Image | Port |
|---|---|---|
| postgres | `postgres:16` | `5432` |
| sync-service | built locally from `deploy/Dockerfile` | `8080` |
| analytics-service | built locally from `analytics-service/Dockerfile` | `8081` |
| export-service | built locally from `export-service/Dockerfile` | `8082` |
| otel-collector (profile `otel`) | `otel/opentelemetry-collector` | `4318` (OTLP HTTP), `9464` (Prometheus exporter) |
| filesystem blobstore | a host-mounted directory | n/a |

Migrations run automatically on `sync-service` startup. The
filesystem blobstore points at `BLOBSTORE_FS_ROOT=/var/lib/snoreguard/clips`
inside the container, mounted from a host directory in `volumes:`.

---

## Staging

Target shape: a single VM or a single-task container runtime, with a
managed Postgres and an object-storage bucket nearby.

| Component | Recommended |
|---|---|
| Compute | One small VM (1 vCPU / 2 GiB) running all three services as systemd units; or one Cloud Run / Fly.io / Railway service per binary |
| Postgres | Cloud SQL / RDS / Neon / Supabase — managed |
| Object store | A cheap S3-compatible bucket (Cloudflare R2 / Backblaze B2 / MinIO single-node) |
| Reverse proxy / TLS | Caddy with auto-Let's-Encrypt, terminating TLS and routing `/sync/*`, `/analytics/*`, `/export/*` to the three services |
| Domain | `staging.snoreguard.example` (TBD) |
| OTel destination | Self-hosted collector forwarding to Tempo / Mimir / Grafana Cloud |

The staging environment is intended to be cheap and disposable — a
canary for the production deploy, not a parallel production.

---

## Production

Target shape: scale-up topology with the same software but with each
component sized for real traffic.

| Component | Recommended |
|---|---|
| Compute | Load-balanced container runtime: ECS, Cloud Run, GKE/EKS, or Nomad. Multi-replica per service. |
| Postgres | Managed: Cloud SQL / RDS with automated backups + read replicas |
| Object store | GCS bucket (private) OR S3 bucket (private, server-side encryption enabled) |
| Reverse proxy / TLS | Vendor LB (CloudFront / Cloud Load Balancing / GCLB) with managed TLS, OR Caddy in front of the LB |
| Domain | TBD (production hostname not yet decided) |
| OTel destination | Tempo / Mimir self-hosted, OR a vendor (Honeycomb / Datadog / Grafana Cloud) |
| Secret manager | Cloud-vendor secret manager (Secret Manager / AWS Secrets Manager) — never env-vars-in-git |
| WAF / rate limiting | Edge-layer rate limiting on top of the per-replica limiter |

---

## Required secrets

Every service reads these from env. In production, every value comes
from a secret manager, never from a file checked into git.

| Secret | Required by | Notes |
|---|---|---|
| `DATABASE_URL` | sync, analytics, export | Postgres connection URL |
| `JWT_SIGNING_KEY` | sync (issue + verify), analytics (verify), export (verify) | ≥ 32 bytes; rotated by adding a key-id and verifying both old + new for an overlap window |
| `APPLE_AUDIENCE` | sync | iOS bundle id; must match the app's |
| `APPLE_ISSUER` | sync | Defaults to `https://appleid.apple.com` |
| `JWT_ISSUER` | sync (issue), analytics+export (verify) | Access-token `iss` |
| `JWT_REFRESH_ISSUER` | sync | Refresh-token `iss`; must differ from `JWT_ISSUER` |
| `BLOBSTORE_BACKEND` | sync, export | `fs`, `gcs`, or `s3` |
| `BLOBSTORE_FS_ROOT` | sync, export (when `fs`) | Directory path |
| `BLOBSTORE_GCS_BUCKET` | sync, export (when `gcs`) | Bucket name |
| `BLOBSTORE_S3_BUCKET` | sync, export (when `s3`) | Bucket name |
| `BLOBSTORE_S3_REGION` | sync, export (when `s3`) | AWS region |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | all three | Empty disables export (no-op providers) |
| Vendor cloud creds | sync, export (when `gcs`/`s3`) | Service account JSON / IAM role / access key — backend-specific |

---

## Database migrations

- `migrations/*.up.sql` files are bundled into the sync-service binary
  via `embed.FS`.
- `migrations.Up()` is called at startup; it applies any not-yet-applied
  migration in order.
- Each applied migration is recorded in `schema_migrations(version,
  checksum)`. If a previously-applied migration's checksum has changed,
  startup aborts rather than silently re-applying — preventing
  accidental drift between an old database and a new binary.
- Down migrations (`migrations/*.down.sql`) exist for every up migration
  and are used for rollback (see below).

---

## Object storage

| Backend | Used in | Notes |
|---|---|---|
| `fs` (filesystem) | dev, staging if you want | Mount-backed; not encrypted at rest by default |
| `gcs` | prod option | Bucket must be private; SSE-default by GCS |
| `s3` (or compatible: R2, MinIO, B2) | prod option | Bucket must be private; SSE must be enabled at bucket level |

The choice between `gcs` and `s3` is deferred to deploy time per ADR
0008. Switching backends does not require code changes — only a config
change (`BLOBSTORE_BACKEND` + the backend-specific creds).

---

## TLS / domain

- **Production**: TLS terminated at the reverse proxy or vendor LB. If
  using Caddy, auto-Let's-Encrypt is the recommended path; if using a
  cloud LB, the vendor's managed-cert flow is recommended.
- **Production domain**: TBD. Current placeholder: `api.snoreguard.example`.
- **Staging domain**: TBD. Current placeholder:
  `staging.snoreguard.example`.
- The iOS app's `APIClient` reads the base URL from build configuration,
  not from a hardcoded literal.

---

## Rollback strategy

- **Deploy by tag**. Every release is a tagged container image:
  `ghcr.io/.../sync-service:vYYYY.MM.DD-N`. Rolling back is
  `kubectl set image` / `gcloud run deploy` / `fly deploy --image` to
  the previous tag.
- **Keep the previous image warm**. Don't delete the prior tag until
  the new one has been stable in prod for 24h.
- **Database rollbacks** via `migrations/*.down.sql`. Apply manually,
  in order, to revert a known-bad migration. Only do this if the
  previous binary cannot run against the current schema; the preferred
  path is to ship a forward-compatible migration.
- **Object store**: rollbacks here are practically impossible (deleted
  bytes are deleted). Object-store actions are append-then-soft-delete
  by default, which limits rollback need.

---

## Backup / restore

- **Postgres**: nightly `pg_dump` to the same object store the app
  uses, encrypted at rest. Retain 30 days. Cloud-vendor managed
  Postgres typically has built-in point-in-time recovery; use that
  in addition to `pg_dump` snapshots.
- **Object store**: enable native versioning (GCS object versioning /
  S3 versioning) so accidental deletes are recoverable for a window.
- **Recovery rehearsal**: quarterly. Restore the latest `pg_dump` into
  a scratch Postgres, point a staging build at it, run the e2e suite.
  Document the result.

---

## Runbooks

Sketch outlines for the incident classes the operator should expect.

### Auth outage (Apple identity-token verification fails)

1. Check `auth_attempts_total{outcome="invalid_token"}` and
   `auth_attempts_total{outcome="store_error"}` rates.
2. If `invalid_token` rate is spiking globally, suspect Apple JWKS
   rotation; sync-service caches JWKS; restart sync-service to refresh.
3. If `store_error` rate is spiking, drop into the DB-unreachable
   runbook.

### DB unreachable

1. `/healthz` on any service returns 503 with `db_error`.
2. Check Postgres availability (cloud console / `psql`).
3. Check connection-pool exhaustion (`pgx` exposes pool stats); restart
   the affected service if the pool is wedged.
4. Check network path from the service container to Postgres.

### Object store unreachable

1. `blobstore_failures_total{op="put"}` rate spikes.
2. Audio uploads start failing with `503 store_error`.
3. Check the bucket's region / IAM / quota.
4. Verify the service's IAM role / access key has not been rotated out
   from under it.

### OTel collector down

1. Trace + metric ingestion stops; spans buffer and then drop in the
   exporter.
2. Restart the collector.
3. If the collector's downstream (Tempo / vendor) is the actual cause,
   the collector will queue and retry per its config; verify the
   exporter has reasonable backoff.

### Refresh-theft spike

1. `refresh_attempts_total{outcome="theft_detected"}` is non-zero.
2. Each occurrence is a real signal — a refresh-token-family revocation
   already happened; the affected user has been forcibly logged out.
3. Investigate: is one user, or many? If many, suspect a leak in the
   client-side storage path or a server-side log-disclosure bug.
4. Page on a sustained rate (≥ 5/min for 5 min).
