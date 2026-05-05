# backend/

Go services for SnoreGuard. Empty stub for now — service skeletons land in
follow-up branches (`backend-sync`, `backend-analytics-export`, phases 4–5 in
`docs/NATIVE_IOS_PORT_PLAN.md`).

Planned services:

- `sync-service` — cross-device sync of sessions, settings, and clip metadata
- `analytics-service` — sleep + snore aggregation, percentile trends, weekly
  insight generation
- `export-service` — CSV / PDF report generation, signed S3 URLs
- `observability` — OpenTelemetry collector + log shipping

Auth is via Apple Sign-In ID tokens verified server-side.
