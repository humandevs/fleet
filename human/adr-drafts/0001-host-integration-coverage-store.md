# ADR-NNNN: Generic per-host third-party integration (coverage) status store

<!-- DRAFT for upstream submission. Copy to docs/Contributing/adr/NNNN-host-integration-coverage-store.md
     with the next sequence number, set Date, and open as its own small PR (Status: Proposed). Keep this
     staging copy in our fork. See human/UPSTREAM-STRATEGY.md §2. -->

## Status

Proposed

## Date

YYYY-MM-DD

## Context

Fleet surfaces some third-party per-host state today, but each source got its **own bespoke table and
card**: `host_munki_info` (Munki), `host_mdm` (MDM enrollment), the MacAdmins card. There is no generic
way to attach normalized third-party **coverage** status to a host — "is this device covered by antivirus?
EDR/MDR? backed up? reachable via remote access? disk-encrypted? patched?" Every new integration that wants
to show such a signal must add yet another one-off table, endpoint, and card, duplicating the
`SetOrUpdate…` + aggregate + read-path pattern each time.

IT/security teams (MSPs especially) increasingly run a stack of such tools and need a single per-host and
fleet-wide view of coverage, plus the ability to find gaps ("show hosts missing managed AV"). Building this
per-vendor does not scale for Fleet or its users.

Constraints: it must not require a premium license (coverage is a core concern), must not alter existing
API contracts, must apply staleness so a source that stops reporting cannot show a device as still
"protected", and must isolate one host's data from another under the standard authorization model.

## Decision

Add a single normalized, vendor-agnostic store: **`host_integration_status`**, one row per
`(host_id, source, category, state)` — "Munki, but for any coverage vendor." Category is a small closed set
(`av`, `mdr`, `remote_access`, `backups`, `disk_encryption`, `patching`); state is a small closed set
(`protected`, `at_risk`, `not_installed`, `unknown`). A source writes only its own rows via a
`SetOrUpdateHostIntegrationStatus` datastore method (mirroring `SetOrUpdateMunkiInfo`); a read API returns a
host's cells and a fleet-wide rollup, with a **freshness gate** that renders any cell older than its
category TTL as `unknown` so stale data never reads as covered.

This ships as three small, independently-reviewable, user-facing PRs, each with an in-tree consumer:

1. The table + types + `SetOrUpdate…`/list/aggregate datastore methods + read endpoint (this ADR).
2. A host-detail **coverage card** + fleet-wide rollup that render the normalized cells.
3. A **host-list filter** by coverage ("only hosts with problems", "missing managed AV").

Fleet core owns the **data model and API only**. *How* cells get populated is out of scope for core: a
first-party job, an out-of-process integration writing via the API, or a third-party tool. Fleet does not
gain a plugin loader or an exported ingestion interface — deliberately, to avoid speculative surface with
no in-tree producer.

## Consequences

**Benefits:**
- One pattern replaces N bespoke per-vendor tables/cards; new coverage sources need zero schema work.
- Users get a single coverage view + gap-finding across any mix of tools.
- Small, additive diffs that copy the Munki/MacAdmins pattern → low review cost.
- Because population is API-driven, third parties can contribute coverage **out-of-process** with no core
  change and no dynamic-code-loading risk for Fleet to own.

**Drawbacks / debt:**
- A new closed enum (`category`) that will occasionally need extension via follow-up PRs.
- Freshness TTLs are per-category constants that must stay in sync between the read gate and the filter
  query (mitigated by a single shared definition).
- The normalized model is intentionally lossy (four states); vendor-specific nuance lives in an optional
  `detail` string, not structured columns.

**Follow-ups:** the card (PR 2) and filter (PR 3); optionally a fleet-wide coverage dashboard tile.

## Alternatives considered

### Status quo — a new bespoke table + card per vendor
Pros: matches existing precedent exactly. Cons: O(vendors) duplication of schema, endpoints, and UI;
no cross-vendor view; every integration re-litigates the same pattern. Not selected: does not scale and
produces more long-term surface than one normalized store.

### A general plugin / extension framework in core
Expose a provider/ingestion interface + registry so integrations register in-tree. Pros: powerful.
Cons: large exported surface, constrains Fleet's refactors, and — decisively — **no in-tree consumer**,
the same objection that led Fleet to reject OpenSpec (ADR-0010). Not selected: speculative framework;
the normalized data store delivers the user value without the framework surface. (Such a registry can live
downstream in a fork and write through the same `SetOrUpdate…` method.)

### Put it behind the premium license
Pros: monetizable. Cons: coverage visibility is a baseline security concern and belongs in core; gating it
fragments the host view. Not selected.

## References

- Fleet feature-request issue: TODO (open first; source of truth)
- Prior art in-tree: `host_munki_info` + the MacAdmins card; `SetOrUpdateMunkiInfo`
- ADR-0010 (OpenSpec, Rejected) — precedent on avoiding speculative surface with no in-tree consumer
