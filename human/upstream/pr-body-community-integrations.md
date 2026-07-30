# DRAFT — PR body for the reference-implementation Draft PR (review before filing)

> Open from `humandevs/fleet:community-integrations-rfc` against `fleetdm/fleet:main`, **as a Draft**,
> only AFTER the feature-request issue exists (fill its number in below). Title suggestion:
> `Community integrations: third-party coverage provider seam + host_integration_status (reference implementation for #TBD)`

---

**Related issue:** Reference implementation for feature request #TBD (not intended to auto-close it).

> **Note for reviewers:** This PR is intentionally opened as a **Draft**, per Fleet's community-PR
> process — it exists as working evidence for the linked feature request, not as a merge request in
> its current shape. If the feature is prioritized into drafting, we'll adapt the implementation
> (naming, module layout — e.g. a `server/integrations/` bounded context following the Activity
> reference pattern — and any UI redesign) to whatever product design lands on.

# Checklist for submitter

- [x] Changes file added for user-visible changes in `changes/`
  (`changes/host-integration-status`, `changes/coverage-security-and-reliability`).
- [x] Input data is properly validated, `SELECT *` is avoided, SQL injection is prevented (all row
  values are placeholders; the only inlined SQL is a constant freshness expression derived from a
  compile-time TTL map over enum category names), and no untrusted data reaches shell commands.
- [x] Timeouts are implemented and retries are limited: every provider HTTP call has a client
  timeout and a response-size cap; a non-200 or oversized response surfaces as an error — never as an
  empty (falsely "uncovered") result. The collector cron registers only when a provider is configured.

## Testing

- [x] Added/updated automated tests: unit tests for every provider (contract, error paths, defensive
  parsing), pure-SQL builder tests, real-MySQL datastore tests, service-layer authz tests, and HTTP
  integration tests.
- [x] Automated tests simulate multiple hosts and test for host isolation: the datastore suite proves
  an upsert touches exactly one `(host_id, source, category)` row (sibling hosts' cells and
  `updated_at` unchanged), and integration tests prove a team-scoped observer can never read another
  team's coverage (rollup and hosts-list filter paths).
- [x] QA'd all new/changed functionality manually: the ScreenConnect provider is live-verified against
  a production instance (1,500+ sessions; timestamp-sentinel and session-filter edge cases found and
  fixed via live testing); the end-to-end loop (agent-deployed vendor tool → vendor API poll →
  coverage cell → dashboard) verified on a test deployment with a real Windows endpoint.

## Database migrations

- [x] Checked schema for all modified tables for columns that will auto-update timestamps during
  migration: `host_integration_status.updated_at` is `TIMESTAMP(6) ... ON UPDATE CURRENT_TIMESTAMP(6)`
  **by design** — refresh-on-write is the mechanism that drives the per-category staleness semantics.
- [x] Confirmed that updating the timestamps is acceptable, and will not cause unwanted side effects
  (the table is new; nothing else reads it).
- [x] Ensured the correct collation is explicitly set for character columns
  (`ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`).

## New Fleet configuration settings

- [x] Setting(s) is/are explicitly excluded from GitOps: all provider configuration is env-var-only
  (`FLEET_COMMUNITY_*`), deliberately kept out of app config and GitOps for this reference
  implementation; nothing is exported by `fleetctl generate-gitops`.

<!-- fleetd/orbit/Fleet Desktop section removed — no agent changes in this PR. -->
