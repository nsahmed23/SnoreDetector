# scripts/

Repo-local helpers. Each script is self-contained — no `node_modules`,
no Go module, just a shebang and the standard library of whatever
runtime it invokes.

## `dev/check-api-contract.sh`

Cross-checks the iOS network client (`Endpoints.swift`) against the
OpenAPI spec (`openapi/snoreguard.v1.yaml`). Parses every endpoint
path literal out of the Swift file, canonicalizes Swift interpolation
(`\(varName)` -> `{varName}`), collapses `{...}` segments, and
confirms each Swift path matches a spec path under template-aware
equality. Exits 1 on drift.

Run it whenever you add an endpoint to the Swift client or to the
OpenAPI spec — the matrix in `docs/API_CONTRACT.md` should also be
updated by hand. Requires `python3` with PyYAML (`pip install
pyyaml` on most distros, or `apt install python3-yaml` on Debian/
Ubuntu).

```bash
./scripts/dev/check-api-contract.sh
```

When `Endpoints.swift` isn't present on the current checkout (e.g.
this branch is off `main` and the iOS client lives on a feature
branch), the script transparently falls back to `git show
origin/claude/ios-network-client:ios/SnoreGuard/Networking/Endpoints.swift`.
That's expected during the multi-branch hardening cycle and should
go away once the iOS client lands on `main`.
