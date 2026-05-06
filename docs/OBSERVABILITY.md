# SnoreGuard observability

SnoreGuard's three Go services emit OpenTelemetry traces + metrics over
OTLP HTTP, plus structured JSON logs via `slog`. This document covers
what we emit, what we redact, what dashboards to build, and what to
alert on.

See ADR 0006 for the rationale behind picking OpenTelemetry. See
`backend/README.md` for the env-var-driven configuration surface and the
list of instruments.

---

## Traces

Span hierarchy per service:

```
HTTP request (otelhttp middleware)
└── handler span ("sync.events.create" / "sync.auth.apple" / ...)
    ├── store query span ("upsert_user", "insert_events", "list_events", ...)
    ├── jwt verify span ("kind=access" / "kind=refresh")
    └── blobstore op span ("blobstore.put", "blobstore.get", "blobstore.delete")
```

Root spans are created by the `otelhttp` HTTP middleware on every chi
route. Child spans are created in:

- Each handler, naming itself via the `endpoint` attribute
  (`sync.events.create`, `sync.auth.apple`, `analytics.summary`,
  `export.events.csv`, etc).
- Each `*store.Store` method.
- Each JWT issue or verify call.
- Each `blobstore.Store` method.

Span attributes follow OpenTelemetry semantic conventions:

| Attribute | Type | Source |
|---|---|---|
| `service.name` | string | `OTEL_SERVICE_NAME` env or service-specific default |
| `http.method` | string | `otelhttp` middleware |
| `http.route` | string | chi route template (e.g., `/events`) |
| `http.response.status_code` | int | handler-set on response |
| `endpoint` | string | hand-named per handler (`sync.events.create`) |
| `user_id_hash` | string | `metrics.HashUserID(uid)` — `sha256(uid)[:8]` hex |
| `inserted` | int | only on event-insert success |
| `outcome` | string | `auth_attempts_total`-style outcome enum on auth/refresh handlers |
| `query` | string | store-query span name |
| `kind` | string | `access` or `refresh` on JWT spans |

---

## Metrics

All metrics live in the `metrics` package (`internal/metrics`). Each
service builds the same instrument set at startup; only the increments
differ.

### Counters (all unit `1`)

| Name | Attributes | Service that increments |
|---|---|---|
| `auth_attempts_total` | `outcome=success\|invalid_token\|store_error\|bad_request\|misconfigured\|issue_failed` | sync |
| `refresh_attempts_total` | `outcome=success\|invalid_token\|expired\|revoked\|theft_detected\|not_found\|store_error\|bad_request\|misconfigured\|issue_failed` | sync |
| `events_ingested_total` | `format=batch` | sync |
| `events_exported_total` | `format=csv\|json` | export |
| `audio_clips_uploaded_total` | `outcome=success\|sha256_mismatch\|too_large\|store_error` | sync |
| `audio_clips_downloaded_total` | `outcome=success\|not_found\|forbidden` | sync |
| `audio_clips_deleted_total` | `outcome=success\|not_found\|forbidden` | sync |
| `audio_bytes_uploaded_total` | (none) | sync |
| `audio_bytes_downloaded_total` | (none) | sync |
| `blobstore_failures_total` | `op=put\|get\|delete\|stat` | sync, export |
| `rate_limit_rejects_total` | `scope=ip\|user` | all |
| `export_budget_rejects_total` | (none) | export |

### Histograms (unit `s`, OTel semantic conv)

| Name | Attributes | Where recorded |
|---|---|---|
| `http_handler_duration_seconds` | `route=<chi route pattern>` | every router (per-request middleware) |
| `store_query_duration_seconds` | `query=<query name>` | each `*store.Store` method |
| `jwt_verify_duration_seconds` | `kind=access\|refresh` | `Issuer.VerifyAccess` / `VerifyRefresh` |
| `blobstore_op_duration_seconds` | `op=put\|get\|delete\|stat` | each `blobstore.Store` method |

---

## Logs

- Format: structured JSON via Go's `log/slog`.
- Handler: `slog.NewJSONHandler(os.Stdout, ...)`.
- Level via `LOG_LEVEL` env (`debug`, `info`, `warn`, `error`); default
  `info`.
- Every log line includes `service.name`, `trace_id`, `span_id` when
  inside a span, plus the route + handler context.
- **Never logged**: raw access tokens, raw refresh tokens, raw user IDs,
  raw audio, clip filenames, request bodies that contain any of the
  above. The standard pattern is to log the
  `metrics.HashUserID(uid)` instead of the UUID.

---

## Dashboards to build (Grafana-style suggestions)

1. **Error rate per route**. Stacked area chart of
   `http_handler_duration_seconds_count{http.response.status_code=~"5.."}`
   over total count, sliced by `route`.
2. **p50 / p95 / p99 handler latency per route**.
   `histogram_quantile(0.95, http_handler_duration_seconds_bucket{route=...})`.
3. **Auth success ratio**.
   `rate(auth_attempts_total{outcome="success"}[5m]) /
   rate(auth_attempts_total[5m])`.
4. **Refresh-theft alarms**.
   `rate(refresh_attempts_total{outcome="theft_detected"}[5m])`.
5. **Export budget exhaustion rate**.
   `rate(export_budget_rejects_total[1h])`.
6. **Blobstore failure rate per op**.
   `rate(blobstore_failures_total[5m])` faceted by `op`.
7. **Audio-clip volume**. `rate(audio_bytes_uploaded_total[5m])` and
   `rate(audio_bytes_downloaded_total[5m])`.
8. **Store query latency p95** by `query`.

---

## Alerting candidates

| Alert | Condition | Why |
|---|---|---|
| 5xx rate spike | `5xx-rate > 1%` over 5min on any service | User-impacting outage |
| Invalid-token spike | `auth_attempts_total{outcome="invalid_token"}` rate > 10× baseline over 5min | Possible credential-stuffing |
| Refresh-theft detected | `refresh_attempts_total{outcome="theft_detected"} >= 1` within 1min | Single occurrence is informative; multiple are urgent |
| Blobstore failures | `blobstore_failures_total` rate > 0 over 5min | Object-store degradation |
| DB unreachable | `/healthz` failing on any service | Postgres outage |
| OTel collector down | exporter heartbeat absent for 5min | Loss of visibility — page on-call to restart |

---

## Redaction rules

| Field | Rule | Constant to grep |
|---|---|---|
| User UUID | Hash before emitting | `metrics.HashUserID` |
| Apple `sub` | Hash before emitting | `metrics.HashUserID` |
| Email | Never emit | (none — never written) |
| Access token | Never emit | (none — never written) |
| Refresh token | Never emit | (none — never written) |
| Audio bytes | Never emit | (none — never written) |
| Clip filename | Never emit | (none — never written) |

The grep contract: searching for `HashUserID` should return every
place a user ID enters a span, log, or metric. Any code path that
attaches a user-derived attribute without going through
`metrics.HashUserID` is a bug.

---

## Allowed / disallowed trace attributes

| Allowed | Disallowed |
|---|---|
| `service.name`, `http.method`, `http.route`, `http.response.status_code` | request body payloads |
| `endpoint` (handler name) | clip filenames |
| `user_id_hash` | raw user UUIDs / Apple `sub` |
| `query` (store query name) | SQL parameter values |
| `kind` (JWT span: access/refresh) | JWT body or signature |
| `outcome` enum on auth/refresh/clip ops | error messages that include token bytes |
| `inserted` (count) | event payloads |
| `op` (blobstore op name) | object-store keys (UUIDs are technically opaque, but conservatively omitted from spans except when explicitly debugging) |
| byte counts | byte content |

---

## SLO proposals

Initial targets, to be revisited with real production traffic:

| Endpoint | SLO |
|---|---|
| `POST /auth/apple` | 99% of requests under 500 ms |
| `POST /events` | 99% of requests under 200 ms |
| `POST /audio/clips` (upload) | 95% of uploads under 10 s |
| `GET /events` | 99% of requests under 200 ms |
| `GET /healthz` (any service) | 99.9% successful, p95 under 50 ms |

These are starting points. Real numbers depend on real traffic patterns
and are open work for M6.

---

## Local OTel collector usage

`docker compose --profile app --profile otel up -d` brings up the OTel
collector alongside the three services. The collector is configured via
`backend/otel-collector-config.yaml` and uses two exporters in dev:

- **`debug` exporter**: prints spans + metrics to stdout. Read with
  `docker compose logs -f otel-collector`. A typical span line:

  ```
  Span #0
      Trace ID       : 1c4f…
      Span ID        : 2a7b…
      Name           : sync.events.create
      Kind           : Internal
      Attributes:
           -> endpoint: Str(sync.events.create)
           -> http.route: Str(/events)
           -> user_id_hash: Str(2a3e9b81f5c4a07d)
           -> inserted: Int(12)
           -> http.response.status_code: Int(200)
  ```

- **Prometheus exporter** on `:9464`. Quick check after driving some
  traffic:

  ```bash
  curl -s localhost:9464/metrics | \
    grep -E '^(events|auth|refresh|http_handler|store_query|jwt_verify|audio|blobstore)'
  ```

The Prometheus exporter is in-memory, so counters reset when the
collector restarts. That is fine for dev. For production, swap the
exporters in `otel-collector-config.yaml` to point at Tempo / Mimir /
the chosen vendor.
