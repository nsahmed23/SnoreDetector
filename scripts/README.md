# scripts/

Developer scripts for the SnoreGuard repo. None of these run in CI;
they are tools for local iteration on a machine the developer
controls.

## Layout

| Subdirectory | Purpose                                                                 |
| ------------ | ----------------------------------------------------------------------- |
| `dev/`       | Backend smoke scripts — exercise the full stack against `docker compose --profile app up -d` via curl. See `dev/README.md`. |

All scripts under this tree refuse to run against non-localhost URLs
and never persist tokens or synthetic payloads to disk. See each
subdirectory's `README.md` for per-script inputs and output format.
