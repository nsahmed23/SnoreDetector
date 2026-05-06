# scripts/dev — backend smoke scripts

Bash scripts that exercise the full SnoreGuard backend stack via curl.
Audience: a developer with a local docker-compose-up-d cluster who wants
to confirm the wire shape of every endpoint before pointing the iOS app
at it.

These scripts run on the developer's machine (or in a sandbox the
developer controls). They are **not** in CI, and they refuse to run
against any non-localhost target.

## Pre-requisites

1. The backend stack is up:
   ```bash
   cd backend
   docker compose --profile app up -d
   # optional: --profile otel for OTel collector + Prometheus scrape
   ```
2. `curl`, `jq`, and `python3` on `PATH`.
3. For the full smoke run, a real Apple identity token in
   `SNOREGUARD_TEST_APPLE_TOKEN`. Apple's signature is the whole
   point of `/auth/apple`; we cannot synthesize a usable token from
   the script. If you only want to confirm the stack is up, use the
   `--mock-auth` flag — that runs the unauthed `/healthz` checks
   and the optional Prometheus scrape, and skips everything that
   needs a bearer token.

## Scripts

### `smoke-backend.sh` — full-stack end-to-end smoke

Hits every endpoint at least once.

| env var                       | default                          | purpose                                    |
| ----------------------------- | -------------------------------- | ------------------------------------------ |
| `BACKEND_BASE_SYNC`           | `http://localhost:8080`          | sync-service base URL                      |
| `BACKEND_BASE_ANALYTICS`      | `http://localhost:8081`          | analytics-service base URL                 |
| `BACKEND_BASE_EXPORT`         | `http://localhost:8082`          | export-service base URL                    |
| `OTEL_PROMETHEUS_URL`         | `http://localhost:9464/metrics`  | Prometheus exporter scrape (optional)      |
| `SNOREGUARD_TEST_APPLE_TOKEN` | *(none)*                         | real Apple identity token (or `--mock-auth`) |

Flags:

- `--debug` — echoes every curl invocation. Tokens are masked
  (`eyJabc...wxyz` style); raw bearer values are never printed.
- `--mock-auth` — skip every step that needs a real Apple Sign-In
  identity token; exits 0 after the unauthed steps. Useful when you
  just want to confirm the three `/healthz` endpoints are up and
  the OTel collector is scraping.

Output format: `[ N/18] description ... OK` per step. Failure prints
the reason and exits non-zero.

### `upload-synthetic-clip.sh` — targeted clip upload

Generates an N-byte random buffer, sha256s it, and posts it via
multipart. Useful for iterating on `POST /audio/clips` without
running the whole smoke.

| input                    | required        | default                  |
| ------------------------ | --------------- | ------------------------ |
| `BEARER_TOKEN` env       | yes             | —                        |
| `BACKEND_BASE_SYNC` env  | no              | `http://localhost:8080`  |
| `--size <bytes>`         | no              | `4096`                   |
| `--mime <type>`          | no              | `audio/m4a`              |
| `--session-id <uuid>`    | no              | mints a session via POST |
| `--debug`                | no              | off                      |

Prints `clip_id`, `object_key`, `uploaded_at`, `created`.

### `export-smoke.sh` — export-endpoint focus

Hits both `/export/events.csv` and `/export/events.json` with all three
`?include=` variants (bare, `sessions`, `sessions,clips`). Validates
section markers in CSV and the array+count-map shape in JSON. Reports
per-response byte sizes so you can sanity-check the per-clip metadata
and per-session columns are actually present.

| input                      | required | default                  |
| -------------------------- | -------- | ------------------------ |
| `BEARER_TOKEN` env         | yes      | —                        |
| `BACKEND_BASE_EXPORT` env  | no       | `http://localhost:8082`  |
| `--debug`                  | no       | off                      |

## Privacy and safety

- **Bearer tokens are read from env vars** and passed to curl via
  `-H "Authorization: Bearer ${TOKEN}"`. They never appear on the
  command line and `--debug` masks them on stderr.
- **Apple identity tokens are masked** under `--debug` to
  `eyJabc...wxyz` (first 6 chars + last 4); the raw token is never
  printed.
- **Synthetic clip bytes never persist.** They live in python's
  memory, get hashed + uploaded + (in `smoke-backend.sh`) downloaded
  for round-trip verification, and are discarded when the script
  exits. No `--data-binary @file` against on-disk content.
- **Localhost-only.** Each script refuses non-`localhost` /
  `127.0.0.1` / `::1` / `0.0.0.0` / `host.docker.internal` URLs at
  startup. There is no `--allow-prod` flag.
- **No real cloud credentials anywhere.** The stack hits the local
  docker-compose backend with `BLOBSTORE_BACKEND=filesystem`. No
  GCS/S3/Apple production secrets ship in these scripts.

## What this confirms

The "what does each endpoint actually do, and what should the iOS
client expect to see" matrix:

| script              | endpoint                              | confirms                                     | metric counter                |
| ------------------- | ------------------------------------- | -------------------------------------------- | ----------------------------- |
| `smoke-backend.sh`  | `GET /healthz` (×3)                   | all three services up + Postgres reachable   | —                             |
| `smoke-backend.sh`  | `POST /auth/apple`                    | Apple verifier accepts a real identity token | `auth_attempts_total`         |
| `smoke-backend.sh`  | `POST /sessions`                      | session upsert + `created` flag              | —                             |
| `smoke-backend.sh`  | `GET /sessions`                       | session shows up in own list                 | —                             |
| `smoke-backend.sh`  | `POST /events`                        | bulk insert returns `inserted=received=N`    | `events_ingested_total`       |
| `smoke-backend.sh`  | `GET /events`                         | events come back via cursor pagination       | —                             |
| `smoke-backend.sh`  | `POST /audio/clips`                   | multipart + sha256 verify path works         | `audio_clips_uploaded_total`  |
| `smoke-backend.sh`  | `GET /audio/clips`                    | clip metadata listing                         | —                             |
| `smoke-backend.sh`  | `GET /audio/clips/{id}/download`      | round-trip sha256 matches                    | `audio_clips_downloaded_total`|
| `smoke-backend.sh`  | `GET /export/events.csv`              | section markers + content-type               | —                             |
| `smoke-backend.sh`  | `GET /export/events.json`             | events/sessions/clips arrays + count map     | —                             |
| `smoke-backend.sh`  | `GET /analytics/totals`               | totals shape                                 | —                             |
| `smoke-backend.sh`  | `GET /analytics/summary`              | days[] in requested tz                        | —                             |
| `smoke-backend.sh`  | `POST /auth/refresh`                  | refresh-token rotation issues new pair       | `refresh_attempts_total`      |
| `smoke-backend.sh`  | `POST /auth/logout`                   | revokes access JTI + refresh family          | —                             |
| `smoke-backend.sh`  | `POST /events` w/ revoked token       | returns 401 (revocation actually checked)    | —                             |
| `smoke-backend.sh`  | `DELETE /audio/clips/{id}`            | soft-delete (skipped when post-logout)       | `audio_clips_deleted_total`   |
| `upload-synthetic-clip.sh` | `POST /audio/clips`            | clip-endpoint isolation testing               | `audio_clips_uploaded_total`  |
| `export-smoke.sh`   | `GET /export/events.{csv,json}` ×6    | all `?include=` variants + shape contract    | —                             |

## Source-of-truth

The wire-format contract lives in `openapi/snoreguard.v1.yaml` (Branch 2).
If a script and the OpenAPI spec disagree, the spec wins; open an issue.

## Implementation note: multipart

`POST /audio/clips` is `multipart/form-data` with a JSON `metadata`
part and a binary `file` part. Bash's printf-based multipart
construction is fragile around binary bodies (CRLF discipline, no
locale-affected pipes, etc.), so both `smoke-backend.sh` and
`upload-synthetic-clip.sh` use a **python heredoc** to build the
multipart envelope in memory and shell out to curl with
`--data-binary @-` so the body is streamed via stdin. The token is
in an `-H Authorization: Bearer …` header, not on the command line.

## Where this script can't go

- `/auth/apple` validates a real Apple identity token. We cannot
  synthesize one from the script — Apple's signature is the whole
  point. Use a captured token in `SNOREGUARD_TEST_APPLE_TOKEN` or
  the `--mock-auth` partial run.
- Rate limits aren't exercised — the smoke run stays well under
  the per-route limits (10/hr clip download, 60/min analytics).
  A separate load-test fixture is the right place for those.
