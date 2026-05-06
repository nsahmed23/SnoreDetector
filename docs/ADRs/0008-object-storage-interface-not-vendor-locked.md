# ADR 0008: Object storage as an interface, not a vendor lock-in

## Status

Accepted.

**Decision update — 2026-05-06:** GCS is chosen as the v1 production
object-storage backend. The blobstore interface remains vendor-neutral.
S3-compatible adapters (R2, MinIO, Backblaze B2) remain valid future
work; the interface contract is unchanged. See `DEPLOYMENT.md` for the
GCP-specific deployment topology.

## Context

Raw audio clips need an object store. The realistic deploy targets are:

- **Local dev** — filesystem (a mounted directory in `docker compose`).
- **Staging** — filesystem on a single VM, OR a cheap S3-compatible
  bucket (R2 / MinIO / Backblaze B2).
- **Production** — GCS or S3-compatible, deploy-target-dependent.

Each of these has a different SDK with a different API shape. If we
hardcode one SDK in the handler layer, switching deploy targets becomes
a refactor, integration tests must spin up a real bucket, and
handler-level tests that touch the store can no longer be unit tests.

## Decision

Define `internal/blobstore.Store` as a small Go interface with the
operations our handlers actually need:

```go
type Store interface {
    Put(ctx context.Context, key string, r io.Reader, sha256 string) error
    Get(ctx context.Context, key string) (io.ReadCloser, error)
    Delete(ctx context.Context, key string) error
    Stat(ctx context.Context, key string) (Metadata, error)
}
```

Implementations:

- `blobstore/fs` — filesystem-backed. Used in local dev, in staging if
  we want, and in tests.
- `blobstore/fake` — in-memory. Used in unit tests so handler tests run
  fast without a filesystem dependency.
- `blobstore/gcs` (future) — Google Cloud Storage.
- `blobstore/s3` (future) — AWS S3 / R2 / MinIO via the AWS Go SDK v2.

The implementation is selected at startup via the `BLOBSTORE_BACKEND`
env var (`fs` / `gcs` / `s3`). Handlers depend only on `blobstore.Store`.

## Consequences

**Positive**:

- Handlers + tests don't change when the deploy target changes. Adding
  a new backend is a new package + a switch case in the config bootstrap.
- Test pyramid stays clean: handler tests use `fake`, integration tests
  use `fs`, end-to-end smoke tests against a real bucket are a separate
  job that runs only on staging deploys.
- The decision of which production backend to use can be made at
  deploy time, not at design time.

**Negative**:

- We are responsible for the interface design. If a backend has a
  primitive we don't model (e.g., GCS-specific resumable uploads with
  CRC32C verification), we either don't use it or we extend the
  interface — the latter requires updating every implementation.
- We give up some vendor-specific features by going through the
  interface. Mitigated by exposing the underlying client when the
  caller really needs it (rare; mostly avoid).

**Neutral**:

- Signed URLs vs proxied download is a per-backend decision. The
  filesystem backend can't sign URLs; the GCS/S3 backends can. We
  default to **proxied** download via
  `GET /audio/clips/{id}/download` so the handler stays uniform; signed
  URLs are a future optimization for high-traffic clip serving.

## Alternatives considered

- **Hardcode AWS S3 SDK across the codebase**. Rejected: optimizes for
  S3-compatibility but locks us to S3 semantics in the handler layer.
  Local dev needs MinIO or LocalStack to run handler tests, which is
  more friction than just reading and writing files. And if we ever
  deploy on Google Cloud Run + GCS, we either pay for an S3
  compatibility layer or rewrite.
- **Hardcode Google Cloud Storage SDK**. Rejected: same problem in the
  other direction. GCS-only locks us out of S3-compatible deploy
  targets including R2 and Backblaze, both of which are reasonable
  budget-friendly choices for a portfolio project.
