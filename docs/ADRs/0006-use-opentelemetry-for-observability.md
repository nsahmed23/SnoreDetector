# ADR 0006: Use OpenTelemetry for backend observability

## Status

Accepted.

## Context

The backend runs three services. We need traces, metrics, and structured
logs in a way that:

- Works locally on a developer laptop without a paid vendor account.
- Targets any production observability backend without rewriting the
  instrumentation.
- Plays well with `chi` HTTP middleware, `pgx` database calls, and JWT
  verification spans.
- Lets us assert no-PII / no-raw-audio rules in code review.

## Decision

Use **OpenTelemetry** with the OTLP HTTP transport. All instrumentation
is built against the OTel Go SDK
(`go.opentelemetry.io/otel/sdk`,
`go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`,
`go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`). Configuration is
fully env-driven via the standard `OTEL_EXPORTER_OTLP_*` variables.

For local dev, the docker-compose ships an `otel-collector` profile with
a `debug` exporter (stdout) and a Prometheus exporter on `:9464`. For
production, swap the exporters in `otel-collector-config.yaml` to point
at any OTLP-compatible destination (Tempo, Mimir, Honeycomb, Datadog,
Grafana Cloud, etc).

## Consequences

**Positive**:

- Standardized API: span attributes follow OpenTelemetry semantic
  conventions (`http.route`, `http.response.status_code`,
  `service.name`, ...). Anything that speaks OTLP can read our traces.
- The same instrumentation runs in dev (debug exporter), staging
  (whatever the staging collector forwards to), and prod (vendor or
  self-hosted Tempo/Mimir).
- Metrics + traces share a single SDK and a single shutdown path.
- Empty `OTEL_EXPORTER_OTLP_ENDPOINT` cleanly disables export (no-op
  providers), so unit tests run without an external collector.

**Negative**:

- The OTel Go SDK is large. Compile times go up; binary size goes up.
- The contrib middlewares occasionally have semver churn. Mitigated by
  pinning versions in `go.mod`.
- OTLP HTTP requires a collector or a vendor endpoint. Mitigated by
  shipping the collector in our compose file.

**Neutral**:

- We commit to never adding raw user IDs, raw audio, or tokens as span
  attributes. Hashed user IDs only (`metrics.HashUserID(uid) → sha256(uid)[:8]`).
  This rule is enforced by code review and by a redaction-rules section
  in `OBSERVABILITY.md`.

## Alternatives considered

- **Vendor lock (Datadog tracer / similar)**. Rejected: tying every
  instrumentation point to one vendor means migration is a rewrite. We
  also lose the OSS collector and the ability to run a fully-local
  observability stack. OTLP gives us "deploy anywhere" by default;
  Datadog's APM agent does not.
- **Syslog / structured-logs only, no tracing**. Rejected: we want to
  follow a request across the three services (sync → analytics → export
  via the same access token), measure handler latency histograms, and
  alert on refresh-theft counters. Logs alone do not give us the
  cross-service trace correlation we want.
