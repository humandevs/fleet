# PR extraction plan: our changes → logical, minimal, testable Fleet PRs

Maps everything we've built to concrete, individually-mergeable PRs. Principle (per the brief): **keep each
core PR as small as possible and let it expose the seam our community plugins do the heavy lifting on** —
**except** the device coverage dashboard, which is a genuinely useful *core* feature and gets pitched
fully, not minimized. Companion: [UPSTREAM-STRATEGY.md](./UPSTREAM-STRATEGY.md),
[adr-drafts/0001-host-integration-coverage-store.md](./adr-drafts/0001-host-integration-coverage-store.md).

## The seam is a REST write endpoint, not a Go interface

The thing our plugins lift on must be an **HTTP write endpoint** on the coverage store — `PUT
/api/latest/fleet/hosts/{id}/integration_status` (upsert this host's cells) — **not** an exported
`HostStatusProvider` Go interface. Why:

- It makes the core dashboard PR **self-justifying**: a complete *populate-and-read* feature, so the table
  isn't speculative (the OpenSpec-rejection trap — a store with no writer).
- It's the extension point Fleet can actually accept: third parties (and our plugins) populate coverage
  **out-of-process** via the API — no dynamic code loading, no exported framework surface.
- Our community `Runner` (fork-side) writes through it (or the compiled-in datastore method); external tools
  write through the same endpoint. One seam, many producers.

**Action:** add this write endpoint (auth: admin/maintainer token; validate category/state enums; upsert)
to Core PR 1. We already have the datastore `SetOrUpdateHostIntegrationStatus`; this exposes it over HTTP.

## Core vs fork boundary

| Stays in our fork (no upstream PR) | Goes upstream (small, testable PRs) |
|---|---|
| `server/community/**` — provider interface, `Collector`, `Runner`, registry, **all** vendor plugins (screenconnect, bitdefender, action1, huntress, veeam, idrive360, warp) | `server/fleet/host_integration_status.go`, `server/datastore/mysql/host_integration_status.go` (+migration), `server/service/host_integration_status.go`, interface/route/mocks, `frontend/**` coverage card + filter, `changes/` |
| `human/**` (design, harness, this plan) | — |

The community plugins are the heavy lifting; upstream gets only the normalized store + read/write API +
the card/dashboard + the filter.

## The PRs (ordered; each independently mergeable & testable)

### PR 1 — Host integration status: device coverage store + read/write API  ⭐ full core feature
The "device dashboard with external information" backbone. **Not minimized** — pitched as a real feature.
- **Files:** `server/fleet/host_integration_status.go` (store types + enums only — *not* `CoverageFilter`,
  that's PR 3); migration `…_AddHostIntegrationStatus.go` (+test); `server/datastore/mysql/host_integration_status.go`
  (`SetOrUpdate`/`List`/`Aggregated` only); `server/fleet/datastore.go` + `service.go` (those methods +
  `HostIntegrationStatus`); `server/service/host_integration_status.go` (read endpoint + staleness +
  **new write endpoint**); `server/service/handler.go` (GET + PUT routes); regenerated mocks;
  `changes/host-integration-status`.
- **Tests:** `TestApplyIntegrationStaleness`, migration up-test, datastore round-trip (`MYSQL_TEST`), write
  endpoint authz/validation test.
- **ADR:** yes — [0001](./adr-drafts/0001-host-integration-coverage-store.md) (introduces the generic pattern).
- **Size:** medium. **Justification:** complete populate+read feature; write API = the documented seam.

### PR 2 — Coverage card on the host details page  (frontend, core feature cont.)
- **Files:** `frontend/interfaces/integration_status.ts` (read types only); `frontend/pages/hosts/details/cards/IntegrationStatus/**`;
  `frontend/utilities/endpoints.ts` (`HOST_INTEGRATION_STATUS` only); `frontend/services/entities/hosts.ts`
  (`getIntegrationStatus` only); `HostDetailsPage.tsx` wiring.
- **Tests:** card component test (`IntegrationStatus.tests.tsx`) + Puppeteer `04-host-details-coverage.png`.
- **Size:** small. Depends on PR 1's read endpoint. Consumes the store → not speculative.

### PR 3 — Filter hosts by coverage ("missing managed AV", "only problems")
- **Files:** `server/fleet/host_integration_status.go` (`CoverageFilter`/`CoverageStatePredicate` +
  `IsZero`); `server/datastore/mysql/host_integration_status.go` (`coverageFreshExpr`, `coverageFilterConds`,
  `ListHostsByCoverage`); `server/fleet/datastore.go` + `service.go` (those methods); `server/service/host_integration_status.go`
  (`HostsByCoverage` endpoint + `coverageFilterFromRequest`); `handler.go` (route); mocks;
  frontend `CoverageFilter/**` + `getHostsByCoverage` + `HOSTS_QUERY_PARAMS.COVERAGE` + ManageHostsPage
  wiring (per [verify/FRONTEND-WIRING.md](./verify/FRONTEND-WIRING.md)).
- **Tests:** `TestCoverageFilterFromRequest`, `TestCoverageFilterConds`, `ListHostsByCoverage` round-trip
  (`MYSQL_TEST`), UI filter Puppeteer shot.
- **Size:** small-medium. **Decision inside:** in-table filtering needs `CoverageFilter` threaded into
  `ListHosts` (MySQL-tested) vs the standalone `/hosts/coverage` endpoint — see FRONTEND-WIRING §"backend".

### PR 4 (optional, own ADR) — `team.parent_id` subteam primitive
Only if we pursue native client/site hierarchy upstream; high-risk, propose via ADR. Otherwise the Org tree
stays fork-side. Not blocking.

## Extraction mechanics (so the cuts stay clean)

- **One feature file spans PRs by hunk.** `host_integration_status.go` (fleet/mysql/service) each hold PR 1
  *and* PR 3 code. Cut per PR by including only that PR's hunks — the store types/methods (PR 1) are
  independent of the filter types/methods (PR 3), so they split cleanly.
- **Never cherry-pick mock diffs.** Regenerate mocks on each PR branch after adding that PR's interface
  methods (`go generate ./server/mock/...`) — the mock file is machine-generated churn.
- **Migration timestamp:** bump on the PR branch if `main` has newer migrations (`/bump-migration`).
- **Don't carry `human/**` or `server/community/**` into any upstream PR** (ADR-0010: no parallel design
  layers; keep review surface minimal). Those are fork-only.
- **Branch order:** PR 1 → PR 2 → PR 3, each cut off `main` after the prior merges (or stacked drafts).
  File a feature-request issue for PR 1 first and let it be drafted before coding.

## Sanity check against the brief

- *Small core PRs exposing the seam the plugins lift on* → the **write endpoint** in PR 1; plugins/3rd
  parties populate through it, all heavy lifting fork-side. ✅
- *Except the device dashboard — a useful core feature* → PR 1 + PR 2 pitched **fully** as the device
  coverage dashboard, not minimized to plumbing. ✅
- *Logical, testable, individual PRs* → 3 (+1 optional), each with its own tests and a clean file/hunk
  manifest. ✅
