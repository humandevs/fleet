# PR: Host integration status (generic third-party coverage)

> The ready-to-open **upstream** PR for the coverage-matrix backbone — the thin, feature-framed slice from
> [PLUGIN-API-RFC.md §7](./PLUGIN-API-RFC.md) / [PLUGINS.md §4.2](./PLUGINS.md). Pitched as a *feature*
> ("generic host integration status, like the MacAdmins card"), **not** as "a plugin system." The
> community providers (ScreenConnect/Bitdefender/…) are **not** part of this PR — they're fork-side
> consumers of the interface. Body below is ready to paste into the Fleet PR (built from
> `.github/pull_request_template.md`).

---

**Related issue:** Resolves # <!-- open a feature issue first per Fleet's drafting requirement -->

## Why

Fleet already surfaces some third-party per-host state — Munki/MacAdmins and MDM — but each got its **own
bespoke table + card** (`host_munki_info`, `host_mdm`, the MacAdmins card). There is **no generic way to
attach normalized third-party *coverage* status to a host** (is this device covered by AV? EDR/MDR? backed
up? reachable via remote access? disk-encrypted?). Every integration that wants to show that today would
add yet another one-off table.

This PR adds a **vendor-agnostic "host integration status"** store + read API — "Munki, but generic": any
integration reports normalized `(source, category, state)` cells for a host, and Fleet renders them on the
host page (and, fleet-wide, as a rollup) exactly like the MacAdmins card. It's small, additive, and follows
the existing `SetOrUpdate…`/aggregate shape.

## What it adds

- **Types** (`server/fleet/host_integration_status.go`): `HostIntegrationStatus` + small normalized enums
  `IntegrationCategory` (`av`/`mdr`/`remote_access`/`backups`/`disk_encryption`) and `IntegrationState`
  (`protected`/`at_risk`/`not_installed`/`unknown`).
- **Table** (`host_integration_status`, migration + test): PK `(host_id, source, category)`, FK to `hosts`
  `ON DELETE CASCADE`, `updated_at TIMESTAMP(6)` for freshness. `COLLATE utf8mb4_unicode_ci`.
- **Datastore** (modeled 1:1 on `SetOrUpdateMunkiInfo`): `SetOrUpdateHostIntegrationStatus`,
  `ListHostIntegrationStatus`, `AggregatedHostIntegrationStatus` (+ regenerated mocks).
- **Service + endpoint**: `GET /api/latest/fleet/hosts/:id/integration_status`, double-authorized (list →
  `HostLite` → read), with a **freshness gate** — a cell older than its category TTL is rendered `unknown`
  so **stale data is never shown as covered** (unit-tested).

**Data poisoning / isolation note:** a provider writes only its own `source`; `host_id` FK-cascades on host
delete; the read path is host-scoped through the standard `authz` double-check, so one host's status never
leaks to another.

# Checklist for submitter

- [x] Changes file added (`changes/host-integration-status`).
- [x] Input validated; **no `SELECT *`**; parameterized queries (placeholders) throughout; no shell interpolation.
- [x] No new network calls in this PR (read path reads the table only) — no timeouts/retries needed here.
- [x] Endpoint is **new** (`…/hosts/:id/integration_status`); no existing path modified → no frontend/CLI break.

## Testing

- [x] Added automated tests: the freshness-gate unit test (`TestApplyIntegrationStaleness`) + the migration
  test (`TestUp_…`, table/FK/cascade).
- [x] Host isolation: FK-scoped by `host_id`; add a datastore round-trip test asserting host A's cells are
  invisible to host B before merge.
- [ ] QA'd manually.

## Database migrations

- [x] New empty table — the `ON UPDATE CURRENT_TIMESTAMP(6)` on `updated_at` has no existing rows to touch,
  so no unwanted timestamp side effects on migrate.
- [x] `COLLATE utf8mb4_unicode_ci` set explicitly on character columns.

## New Fleet configuration settings

- [x] None — this PR adds no server config setting (excluded from GitOps by virtue of not existing).

## fleetd/orbit/Fleet Desktop

- [x] Server-side only; no agent/fleetd change.

---

## Cutting the branch (fork-side → upstream)

This feature was built on `human-dev` (which also carries our fork-only community providers + `human/`
docs). To open the **clean upstream PR**, extract **only** these files onto a branch off `fleetdm/fleet`
`main` — **not** `server/community/*` and **not** `human/*`:

```
server/fleet/host_integration_status.go
server/fleet/datastore.go                         # the 3 added interface methods
server/fleet/service.go                           # the added HostIntegrationStatus method
server/service/host_integration_status.go (+_test)
server/service/handler.go                         # the added route
server/datastore/mysql/host_integration_status.go
server/datastore/mysql/migrations/tables/2026…_AddHostIntegrationStatus.go (+_test)
server/mock/datastore_mock.go                     # regenerate on the target branch
server/mock/service/service_mock.go               # regenerate on the target branch
changes/host-integration-status
```

Steps: branch off upstream `main` → apply the files above → **bump the migration timestamp** if `main` has
newer migrations (`/bump-migration`) → `make generate-mock` on that branch → `go build ./... && go test
./server/service/ -run TestApplyIntegrationStaleness` → commit with this description → push to your fork →
`gh pr create`. **RFC/issue first** per Fleet's drafting requirement.

Honest merge odds: **low-to-medium** (per PLUGINS.md §4.3) — best if pitched as the user-facing coverage
feature with one real first-party consumer and tests, never as "a plugin system."
