# ADR 0003: Use Go for the backend services

## Status

Accepted.

## Context

The backend has three concerns: identity (Apple Sign-In + JWT issue +
refresh rotation), state (Postgres for users / sessions / events / clip
metadata), and IO (object-store multipart upload / download, streaming
CSV/JSON export). It is operated by a single engineer. It must:

- Compile to a small static binary that is straightforward to deploy in
  any container runtime.
- Have a strong stdlib + ecosystem for HTTP routing, JSON, structured
  logging, JWT, OTel, and Postgres.
- Be approachable for portfolio review — recruiters can read Go.
- Run integration tests against a real Postgres in CI without exotic
  dependencies.

## Decision

Implement the three services (`sync-service`, `analytics-service`,
`export-service`) in Go, single module, three `cmd/` binaries, sharing
internal packages (`apple`, `auth`, `jwt`, `store`, `httpkit`, `otel`,
`config`, `metrics`, `blobstore`, `migrations`).

## Consequences

**Positive**:

- Single static binary per service; distroless container images; trivial
  `docker compose` for local dev.
- `chi` + `pgx` + `slog` + `golang.org/x/crypto/jwt`-class libraries are
  battle-tested.
- First-class OTel SDK (`go.opentelemetry.io/otel`) with HTTP +
  database instrumentation maintained by the OTel community.
- Goroutines + `context.Context` map cleanly onto the request-scoped
  cancel + timeout patterns we want for export streaming.
- Easy to read for reviewers — Go is a small language by design.

**Negative**:

- Less expressive type system than Rust; the JSON-decode + validate
  paths require boilerplate.
- Generics are still relatively new; pattern is "not too many".
- No async/await; concurrency is goroutines + channels, which is fine
  for our IO patterns but unfamiliar to reviewers from Node/Python.

**Neutral**:

- We commit to keeping handler-level logic free of `panic`s, since Go
  panics propagate up unless explicitly recovered in middleware.

## Alternatives considered

- **Rust backend (`axum` / `actix`)**. Rejected: would let us share
  types with the detector core but the operational story is heavier
  (longer compile times, more careful async/await ergonomics, smaller
  pool of recruiters comfortable reading it). The detector-core /
  backend boundary is HTTP-shaped, not type-shaped — there is no real
  type-sharing win.
- **Node.js (`express` / `fastify`)**. Rejected: introduces a JS runtime
  on the production path. JWT, Postgres, and OTel all have working
  Node libs but the operational footprint (memory, startup time,
  dependency tree) is materially heavier than a static Go binary.
- **Python + FastAPI**. Rejected: no static binary, harder containerization
  for low-resource deploy targets, and the ergonomic JWT/OTel ecosystem
  is fragmented. FastAPI is great for ML-adjacent backends; ours is
  not ML-adjacent.
