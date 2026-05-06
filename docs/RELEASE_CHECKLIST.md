# SnoreGuard release checklist

Pre-submission gate. **Every** item below must be green before clicking
**Submit for review** in App Store Connect. Use the boxes as a
literal checklist on the release branch's PR description.

For background see [`V1_SCOPE.md`](./V1_SCOPE.md) (what's actually
shipping) and [`MAC_QUICKSTART.md`](./MAC_QUICKSTART.md) (how the
release engineer reproduces a build locally).

---

## Code state

- [ ] All in-flight PRs targeting the release branch are reviewed and
      merged, or explicitly flagged as out-of-scope and closed.
- [ ] The release branch has zero draft PRs.
- [ ] No TODO / FIXME comments added in the last sprint reference
      release-blocking work.
- [ ] `git log main..release/v1` reads cleanly — squash + rebase
      noise commits before tagging.

## CI green

CI must be green on the **merge commit**, not just the head of the
branch. Check the Checks tab on the merge commit's PR:

- [ ] `rust.yml` — fmt, clippy, test, xcframework dry-build
- [ ] `go.yml` — vet, test, integration test against ephemeral Postgres
- [ ] `integration.yml` — cross-service smoke tests
- [ ] `web.yml` — TS build + lint of the marketing-site spec app
- [ ] `ios.yml` — when the iOS workflow becomes real (currently
      Linux-only syntax checks); the Mac runner job must pass on the
      release commit.

## Decisions applied

These have to be resolved (a, b, c, or d picked) and the answer
encoded in code, not just a doc:

- [ ] **HealthKit r/w status**: which option (a/b/c/d) from the
      product decision did we ship? The three toggles in Settings
      reflect the choice.
- [ ] **Object-storage backend** per [ADR 0008](./adr/0008-object-storage.md):
      local filesystem for staging, S3-compatible for production. The
      `OBJECT_STORE_DRIVER` env var is set to the production value in
      the deploy manifest.
- [ ] **Crash-reporting backend**: vendor selected (Sentry / Bugsnag /
      none); SDK initialized in `App.swift`; DSN in the build's
      Release config; signed by the privacy team.
- [ ] **Privacy-policy URL** is live and serving `PRIVACY_POLICY.md`
      content over HTTPS at the URL we list in App Store Connect.

## iOS provisioning

- [ ] The App ID `com.snoreguard.app` has these capabilities enabled
      in the developer portal: HealthKit, App Groups
      (`group.com.snoreguard.shared`), Sign in with Apple,
      Push Notifications (if shipped).
- [ ] The distribution provisioning profile is current (not within 30
      days of expiry).
- [ ] Xcode → Signing & Capabilities shows the correct team and
      profile for the **Release** configuration.
- [ ] The IPA built from `Product → Archive` validates against App
      Store Connect (Organizer → **Validate App** is green).

## Backend deployment

- [ ] Production environment provisioned (Postgres, object storage,
      OTel collector, ingress).
- [ ] Secrets are in the production vault, not in `.env` files:
      `JWT_SIGNING_KEY`, `APPLE_AUDIENCE`, `DATABASE_URL`,
      `OBJECT_STORE_*`, OTel auth.
- [ ] Database migrated to the schema version this release expects;
      `schema_migrations` table reflects the latest applied migration.
- [ ] Object storage bucket / volume exists, has the expected
      lifecycle policy (clip retention per `PRIVACY_POLICY.md`), and
      the service account has read+write+delete on it.
- [ ] Rate limiter and per-user export budget thresholds match the
      values committed in `internal/config`.

## Privacy assets

- [ ] [`PRIVACY_POLICY.md`](./PRIVACY_POLICY.md) is published at the
      URL listed in App Store Connect; renders correctly on mobile;
      effective date is the release date, not `<TBD>`.
- [ ] App-Store **App Privacy** nutrition labels match
      `PRIVACY_LABELS.md` (Phase A, sibling PR).
- [ ] [`DISCLAIMER.md`](./DISCLAIMER.md) is reachable from the in-app
      Settings → About screen and from the privacy-policy page footer.
- [ ] Cross-references between `PRIVACY_POLICY.md`,
      `PRIVACY_AND_DATA_LIFECYCLE.md`, and `DISCLAIMER.md` resolve.

## Screenshots

- [ ] Captured for all required device classes per
      `SCREENSHOTS_PLAN.md` (Phase A, sibling PR): 6.7" iPhone, 6.1"
      iPhone, 5.5" iPhone (legacy), 12.9" iPad Pro (if iPad supported).
- [ ] Each screenshot uses canned data (no real user PII) per the
      capture script.
- [ ] Captions match the marketing-copy doc.

## TestFlight smoke test

- [ ] At least one **external tester** (not a developer on the
      project) installed the build via TestFlight and completed the
      full flow:
  1. Sign in with Apple
  2. Record a session that fires events
  3. Confirm events sync to the backend
  4. Confirm a HealthKit write landed (visible in Health.app)
  5. Export their data via the in-app **Export** button
  6. Delete a clip
- [ ] Tester reported no crashes; verified against
      `crash-reporter.console`.

## Release notes

- [ ] `METADATA.md` (Phase A, sibling PR) "What's New" template is
      populated for **this** version, not a template placeholder.
- [ ] Release notes mention any new system permissions the app will
      ask for (HealthKit toggles, audio clips upload).

## Rollback plan

- [ ] Previous TestFlight build is retained — not expired or removed.
- [ ] Previous backend container image is tagged with its release
      version (`backend-sync:v1.0.0`, `…analytics:v1.0.0`,
      `…export:v1.0.0`) so a `kubectl rollout undo` (or equivalent)
      reverts to the prior known-good cleanly.
- [ ] Database migrations introduced in this release have a tested
      down-migration path, OR are documented in the rollback runbook
      as forward-only and acceptable to leave in place during a
      revert.
