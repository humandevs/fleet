# DRAFT — Feature request for fleetdm/fleet (review before filing)

> File as a **feature request issue** on fleetdm/fleet (their documented intake for product changes).
> ADR-style structure per our plan. Before filing: replace `#TBD` with the draft PR number, attach the
> two screenshots (host-details Coverage card + the /coverage grid), and give it a final read.

---

**Title:** Third-party integration coverage: know whether every host is *actually* protected by the AV/EDR/RMM/backup tools you deploy

## Status

Proposed (community). A working reference implementation exists — see the intentionally-**Draft** PR #TBD.
We know product-changing contributions go through Fleet's drafting process; the PR is evidence for this
request, not a merge request. If this gets prioritized, we're happy to adapt the implementation to
whatever shape drafting lands on — or for Fleet to take the concept and design its own surfaces.

## Context

Fleet deployments in MSP and mid-market environments almost always run third-party agents next to
fleetd: AV/EDR (Bitdefender, Defender), MDR (Huntress), remote access (ScreenConnect), patching
(Action1), backup (Veeam). Fleet's software inventory answers *"is the binary present?"* — but it
cannot answer the question operators actually care about: **"is this host enrolled, checked-in, and
healthy according to the vendor itself?"**

The failure mode this hides is common and dangerous: the EXE is present, so inventory looks fine — but
the service is stopped, tamper-protection killed the agent, or the device never enrolled vendor-side
and hasn't checked in for weeks. Today teams close this gap with per-console eyeballing or spreadsheets
that cross-reference each vendor console against Fleet — precisely the kind of toil Fleet exists to
eliminate.

What's needed is a small, generic notion of per-host **coverage**: for each protection category a host
is *expected* to have, is it present, healthy, and **fresh** — and which hosts have gaps? (Following
RMM convention: *no data means RED*, so a host no provider has ever reported is a problem device, not
an unknown-unknown.)

This is domain-shaped like Fleet's modular-monolith direction
(`docs/Contributing/architecture/modular-monolith/README.md`): third-party integration status is a
cohesive business domain with clear boundaries, in the spirit of the Activity bounded context
(ADR-0007).

## Decision (proposed)

A free-tier, additive **integration coverage** capability:

- **A provider seam**: a `HostStatusProvider` interface (`Source()`, `Categories()`) with an optional
  `Collector` (`Collect(ctx) ([]HostStatusReport, error)`), plus a registry and a runner that resolves
  vendor-side device identifiers (hostname / serial / UUID) to Fleet hosts and upserts coverage cells.
  Vendor API specifics stay entirely inside providers; the core never imports a vendor.
- **One table**: `host_integration_status` — one row per `(host_id, source, category)` with
  `state ∈ {protected, at_risk, not_installed, unknown}` across six categories
  (`av, mdr, remote_access, backups, disk_encryption, patching`), `detail`, and `updated_at`
  (bumped on every successful write — this drives freshness).
- **Freshness as a first-class semantic**: per-category TTLs (e.g. remote access 1h, AV/MDR 2h,
  backups 36h) derived from a single source of truth in both the service read-path and the SQL
  filters/rollup. A stale cell reads as `unknown` — **a dead integration can never render as
  covered.** Never-reported hosts count as problem devices.
- **Read surfaces**: per-host cells (`GET /hosts/{id}/integration_status`); a viewer-team-scoped
  aggregate rollup (`GET /host_integration_status/summary`, matching `/macadmins` semantics:
  team_id=0 = "no team", unknown team = 404); coverage filters on the **standard hosts list**
  (`coverage_problems=true`, missing-category, category+state) so they compose with every existing
  hosts-list filter; and an opt-in `populate_integration_status` on `GET /hosts` that powers a
  one-row-per-host coverage grid.
- **Zero impact when unused**: the collector cron registers only when at least one provider is
  configured — stock deployments run no new cron, and misconfiguration disables the collector with a
  loud log rather than aborting boot.
- **No plugin framework**: registrations are compile-time modules spread into core registration
  points via one-line hooks (CSP-compatible; no runtime loading; no new dependencies).

Authorization: coarse authz gate plus mandatory viewer team-filter applied in SQL, with regression
tests proving a team-scoped observer can never read another team's coverage.

If Fleet prefers a narrower first step, the interface + table + read surfaces stand alone; reference
providers can live out-of-tree indefinitely.

## Consequences

**Positive**
- Closes a real observability gap free-tier, for every deployment running third-party security tools.
- Additive: ~85 files, +5.7k/−5, one migration, no new Go or JS dependencies, EE untouched.
- Providers are small and defensive (~300 lines each: timeouts, response-size caps, non-200 → error —
  a failed vendor poll can never masquerade as "no hosts covered").
- The freshness/"no data = RED" semantics encode operational best practice into the data model rather
  than leaving it to each consumer.

**Negative / costs**
- A new table and a six-category/four-state vocabulary to maintain.
- Vendor API drift risk — contained inside providers, which no-op unless explicitly configured.
- New UI surfaces (host-details Coverage card, dashboard tile, /coverage grid) that Fleet product
  design would likely reshape — expected and welcome.

**Neutral**
- Nothing premium-gated changes; configuration is env-var-only in the reference implementation
  (deliberately excluded from GitOps until/unless drafting decides otherwise).

## Reference implementation

Draft PR: #TBD — the seam, the table + migration, the read surfaces, a live-verified ScreenConnect
provider (tested against a production instance managing 1,500+ endpoints), a Bitdefender GravityZone
provider, the coverage UI, and the test suite (unit; real-MySQL datastore tests including
host-isolation; HTTP integration tests including the team-isolation regression guards).

*Screenshots:* [attach: host-details Coverage card; /coverage per-host grid]

## References

- `docs/Contributing/architecture/modular-monolith/README.md` (bounded contexts; Activity as the
  shipped reference pattern, ADR-0007)
- Freshness/staleness prior art: dead-man semantics common to RMM/NMS platforms ("no data ⇒ RED")
- Fleet community PR process (`handbook/engineering#review-a-community-pull-request`) — followed here
  deliberately: issue-first, PR self-set to Draft.
