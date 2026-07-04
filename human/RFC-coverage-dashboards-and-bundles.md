# RFC: Host coverage dashboards, status filtering, and integration bundles

> Status: **draft**. Author: Human-ISM. Scope: the generic Fleet-core mechanism we propose to contribute
> upstream, plus the fork-side community-plugin + bundle layer we build on top of it. Companion docs:
> [PLUGINS.md](./PLUGINS.md) · [PLUGIN-API-RFC.md](./PLUGIN-API-RFC.md) · [MVP.md](./MVP.md) ·
> [OSS.md](./OSS.md) · [pr-host-integration-status.md](./pr-host-integration-status.md) ·
> [RISK-REGISTER.md](./RISK-REGISTER.md).

## 1. Summary

Three capabilities, one spine:

1. **Coverage dashboard** — a vendor-agnostic per-host matrix of normalized coverage cells
   (AV / MDR / Remote access / Backups / Disk encryption), rendered as status icons, fed by pluggable
   integrations. *(Backbone already merged as `host_integration_status`.)*
2. **Status filtering** — N-able-style saved views: "only hosts with problems", "only hosts missing
   Managed AV", etc., over those coverage cells.
3. **Integration bundles** — per client/site desired-state packages (e.g. *Standard MSP* = Bitdefender +
   policy, Backups, ScreenConnect, Patching, a software list) with **parent→child inheritance and
   overrides**, spanning both Fleet-native resources and our community plugins.

The mechanism (1+2, and the config-inheritance primitive under 3) is **generic and upstream-contributable**.
The concrete vendor plugins and the MSP bundle catalog are **fork-side** (our repo, permissively licensed).

## 2. Motivation & contribution narrative

We are migrating an existing MSP/RMM+MDM practice onto Fleet. Our former tool gave us, per client/site:
a coverage board, "show me what's broken" filtering, and inheritable service bundles. Fleet has the data
plane (osquery, MDM, software, teams) but not these three cross-vendor product surfaces.

Rather than keep them proprietary, we propose contributing the **generic seams** upstream and shipping the
**vendor plugins we already wrote** (adapted to those seams) as open source on our GitHub, as a working
demonstration. Our roadmap is to migrate the remaining custom integrations from our old tool the same way —
generic mechanism upstream, vendor specifics as community plugins — to grow Fleet core/EE adoption in the
MSP segment.

**Licensing.** Generic mechanism → Fleet's MIT core (the upstream PR). Community plugins → our GitHub,
permissively licensed (Apache-2.0), © Human-ISM. They **do not require Fleet EE** and coexist with it
([PLUGINS.md](./PLUGINS.md)). No EE source is read or copied (clean-room).

## 3. What is upstream vs fork-side

> **Revised after studying Fleet's process (see [UPSTREAM-STRATEGY.md](./UPSTREAM-STRATEGY.md)).** Fleet
> rejected OpenSpec (ADR-0010) for adding review surface + maintenance with *no in-tree consumer*. An
> exported provider interface + Runner with zero in-tree producers hits that same objection — so the
> **ingestion seam is fork-side**. Only the normalized **data** seam (table + read/write + dashboard +
> filter) is upstream-candidate: third parties write to it out-of-band; the interface/registry/runner are
> ours.

| Piece | Home | Rationale |
|---|---|---|
| `host_integration_status` table + types + read/write API | **Upstream (MIT)** | Generic "Munki, but for any coverage vendor" — a data store with an in-tree consumer (the host card). *Merged.* |
| Coverage-matrix card + `ColumnSpec` (icons/labels) | **Upstream (MIT)** | Renders normalized cells; concrete UI feature, no vendor knowledge. |
| Host-list coverage filter (`ListHostsByCoverage`) | **Upstream (MIT)** | Concrete "filter hosts by coverage" feature over the table. |
| `HostStatusProvider`/`Collector` + `Runner` + registry | **Fork (Apache-2.0)** | Ingestion seam has no in-tree producer → speculative framework upstream. Writes via the upstream `SetOrUpdate…` method. |
| ScreenConnect / Bitdefender / Action1 / Veeam / iDrive360 plugins | **Fork (Apache-2.0)** | Vendor-specific; our contribution to *demonstrate* the seams. |
| Dashboards-as-plugins, Views/Triggers engines, bundles, Org tree | **Fork** | Framework/product layers; not minimal in-tree features. |
| `team.parent_id` (subteams primitive) | **Upstream candidate (ADR)** | *If* pitched as one small nullable column; high-risk, propose via ADR. Else fork-side Org tree. |

## 4. Provider model (built)

Registration declares columns; a data-bearing provider additionally implements `Collector`:

```go
type HostStatusProvider interface {         // what columns this plugin owns
    Source() string
    Categories() []fleet.IntegrationCategory
}
type Collector interface {                    // optional: can pull vendor data
    Collect(ctx context.Context) ([]HostStatusReport, error)
}
```

`HostStatusReport{Identifier, IdentifierKind, Category, State, Detail}` is vendor data keyed by a host
handle (hostname/UUID/serial). The **`Runner`** resolves the identifier to a Fleet host
(`HostByIdentifier`/`HostByUUID`) and upserts the cell — the *only* DB-touching component, so plugins stay
pure and unit-testable against an httptest vendor server. Missed runs degrade to "unknown" (freshness gate),
never stale "protected".

**Reference plugin — ScreenConnect (functional today):** deploys the org-linked access agent (positional
`CustomProperty1..8` = client/site handoff, self-hosted relay overrides), and `Collect` polls the RESTful
API Manager, mapping `GuestConnectedCount>0 → protected` into the `remote_access` column. See
[setup/screenconnect.md](./setup/screenconnect.md).

Vendor plugins to follow the same shape: **Bitdefender GravityZone** (`av`+`mdr`, JSON-RPC
`getManagedEndpointDetails`), **Action1** (patching — its own plugin shape, not a coverage column),
**Veeam / iDrive360** (`backups`).

## 5. Coverage dashboard (custom fields / icons)

Columns come from the union of registered providers' `Categories()`. Each category renders as a status
icon with a fixed, colorblind-safe vocabulary:

| State | Icon | Meaning |
|---|---|---|
| `protected` | ✓ green | covered & healthy |
| `at_risk` | ! amber | present but degraded (offline / policy drift / stale backup) |
| `not_installed` | ✕ red | expected but absent |
| `unknown` | – grey | no data or stale past TTL |

**Custom fields/icons** are declared, not hardcoded, via a display spec so a plugin controls its column's
label/icon/order without a UI change:

```go
type ColumnSpec struct {
    Category fleet.IntegrationCategory
    Label    string // "Managed AV", "Remote access"
    Icon     string // icon key in the design system
    Order    int
}
// Provider optionally implements: ColumnSpecs() []ColumnSpec
```

Cell `Detail` (e.g. "last seen 3m ago", "agent 7.9.1", "last backup 26h ago") surfaces on hover. Per-host
page shows the row; fleet-wide dashboard shows the rollup (`AggregatedHostIntegrationStatus`).

## 6. Status filtering (N-able-style views)

Goal: "show me only hosts with problems" / "only hosts missing Managed AV" / "AV at-risk on the Acme
fleet". A host-list filter over `host_integration_status`, layered on Fleet's existing host list.

**API (additive query params on the host list):**

```
GET /api/latest/fleet/hosts?coverage=problems
GET /api/latest/fleet/hosts?integration_category=av&integration_state=not_installed
GET /api/latest/fleet/hosts?coverage=missing:av,mdr        # not "protected" in any listed category
```

**Filter model (built — `fleet.CoverageFilter`):**

```go
type CoverageFilter struct {
    Problems          bool                     // any effectively-non-protected cell (wrong state OR stale)
    MissingCategories []IntegrationCategory    // no fresh "protected" cell for EACH listed category
    StatePredicates   []CoverageStatePredicate // exact (category,state); "unknown" also matches stale
}
```

**SQL shape (anti-join / EXISTS, staleness-aware) — built & unit-tested (`coverageFilterConds`):** a host
matches "missing AV" when **no** fresh `host_integration_status` row exists with
`category='av' AND state='protected' AND <fresh>`. "Problems" = `EXISTS` a cell that is non-protected **or**
stale. Freshness is applied **in SQL** via a constant per-category-TTL `CASE` (mirrors the read-path gate),
so a stale "protected" can't hide a real gap — the one subtlety; see [RISK-REGISTER.md](./RISK-REGISTER.md)
\#3. Expected categories for "missing" ultimately come from the host's **bundle** (§7).

*Built:* `fleet.CoverageFilter` + `coverageFilterConds` (pure SQL builder, unit-tested) +
`Datastore.ListHostsByCoverage` (returns matching host IDs). *Staged:* promote to the `fleet.Datastore`
interface, hydrate/paginate through the standard host list (order-key allowlist), and a `MYSQL_TEST`
round-trip — done alongside the frontend so it's verified end-to-end.

## 7. Integration bundles + inheritance

### 7.1 Does Fleet already do this? Partly.

Fleet has **teams** (per-team config profiles, software, policies, scripts, queries) and **GitOps**. But
teams are **flat two-level** (global + team) — there is **no** client→site→device hierarchy with per-level
override chains, and nothing that composes **community-plugin** config (Bitdefender policy, backup plan,
patch ring) alongside Fleet-native resources. That inheritance layer is the gap this section fills. We
**keep Fleet teams as the execution substrate** and add the hierarchy on top, compiling down to it.

### 7.2 Model

```go
// A Bundle is a named desired-state package at one hierarchy level. Levels form a tree:
//   Global (built-in default) → Client → Site.  A host inherits its Site's effective bundle.
type Bundle struct {
    ID       string
    Name     string   // "Standard MSP", "Acme — HQ"
    Parent   string   // parent Bundle ID ("" for Global)
    Level    Level    // global | client | site

    // Fleet-native desired state (compiled to a Fleet team / GitOps):
    ConfigProfiles []ProfileRef
    Software       []SoftwareRef
    Policies       []PolicyRef
    Scripts        []ScriptRef

    // Community-plugin desired state: plugin Source() -> opaque typed config (json.RawMessage).
    // e.g. "bitdefender" -> {policyId, modules}; "action1" -> {ring, maintenanceWindow};
    //      "veeam" -> {backupPlan}; "screenconnect" -> {company, site}.
    Plugins map[string]json.RawMessage

    // Per-key removals so a child can DROP an inherited item (not just override).
    Remove []string
}

type Level string // "global" | "client" | "site"
```

### 7.3 Resolution (pure, testable)

Effective bundle for a host = fold from root to leaf:
`resolve(global, client, site)` where each child **overrides by key** and may **remove** inherited keys.

- Scalars / typed plugin configs: **last writer wins** (child replaces parent for that plugin `Source`).
- Lists (software, profiles): **merge by ref ID**; child entry replaces same-ID parent entry; `Remove`
  drops an inherited ID.
- Plugin configs: **deep-merge per plugin** via the plugin's own merge (default: child replaces). A plugin
  may opt into field-level merge by implementing `MergeConfig(parent, child) (json.RawMessage, error)`.

This resolver is a pure function (`ResolveBundle(chain ...Bundle) EffectiveBundle`) — unit-testable with
no I/O, and the natural upstream-candidate primitive.

### 7.4 Applying a bundle

Two sinks for the resolved bundle:

1. **Fleet-native** → compile to a concrete Fleet **team** (map Site→team) + GitOps config, using existing
   Fleet APIs. No new execution engine; we render into what Fleet already runs.
2. **Community plugins** → an optional `Applier` seam so a plugin pushes its slice of desired state to the
   vendor:

```go
type Applier interface {
    // Apply pushes this plugin's resolved config to the vendor for the given targets (idempotent).
    // e.g. bitdefender: assign policy to endpoints; action1: set patch ring; screenconnect: EnsureSessionGroup.
    Apply(ctx context.Context, config json.RawMessage, targets []fleet.Host) error
}
```

Symmetry with §4: `Collector` reads vendor→Fleet (status), `Applier` writes Fleet→vendor (config). Same
plugin, two optional halves. ScreenConnect's `EnsureSessionGroup` (already stubbed) is the first `Apply`
target — provisioning the client/site group when a bundle is assigned.

## 8. Security & isolation

- **Secrets** (API keys, shared secrets) are envelope-encrypted (KMS), never plaintext `app_config_json`
  ([RISK-REGISTER.md](./RISK-REGISTER.md) #3).
- **Multi-tenant**: a provider writes only its own `Source`; `host_id` FK-cascades; reads are host-scoped
  through the standard authz double-check. Bundles are scoped per client/site; cross-tenant bundle
  references are rejected at resolve.
- **Blast radius**: one provider's failed Collect/Apply is logged and skipped — never aborts the sweep or
  another tenant.

## 9. Build status & plan

| # | Item | State |
|---|---|---|
| 1 | `host_integration_status` table + read API + freshness gate | ✅ built (upstream PR drafted) |
| 2 | `HostStatusProvider`/`Collector` + `Runner` (+ tests) | ✅ built |
| 3 | ScreenConnect plugin — deploy + functional `Collect` (+ httptest) | ✅ built |
| 4 | Bitdefender GravityZone `Collect` (`av`+`mdr`, JSON-RPC, + httptest) | ✅ built |
| 5 | Action1 — agent deploy + patch monitoring/staleness `Collect` (`patching`, + httptest) | ✅ built |
| 6 | `CoverageFilter` + `coverageFilterConds` (freshness-aware SQL) + `ListHostsByCoverage` | ✅ built (endpoint wiring staged) |
| 7 | Veeam / iDrive360 `Collect` (`backups`) | ⏳ next |
| 8 | `ColumnSpec` + frontend coverage matrix + filter UI | ⏳ next |
| 9 | Client/site substrate (§11) + `ResolveBundle` resolver | ⏳ next |
| 10 | Bundle → Fleet team/GitOps compiler + `Applier` seam | ⏳ later |
| 11 | Views/Triggers automation engine (§10) | ⏳ later |

**PR sequencing (upstream):** land #1 (drafted) → #2 (interface+runner, no vendor code) → #8 dashboard →
#6 filtering → optionally the §7 resolver. Vendor plugins (#3–#5, #7) ship on our GitHub, not upstream.
Each upstream PR is framed as a user-facing feature with one real consumer + tests — never as "a plugin
system" ([PLUGINS.md §4.3](./PLUGINS.md)).

## 10. Views + Triggers automation engine (pluggable)

A Zendesk-style split, deliberately **loosely coupled so multiple engines can coexist** (built-in, Zapier,
n8n, …) — the coverage data and the automation are separate concerns joined only by events + a filter DSL.

- **Views = saved filters.** A `View` is a named predicate over hosts/coverage (superset of
  `CoverageFilter`: coverage cells + host facts + labels/teams). Views back both dashboard tabs and Trigger
  scoping. Reused, not reinvented, by every engine.
- **Triggers = event → (View gate) → action.** A `Trigger` fires on an **event** (coverage cell changed,
  went at_risk, N hours stale, patch deploy failed), is **gated by a View** (only Acme HQ; only `patching`
  at_risk), and runs an **action**. "Views filter Triggers" = the View is the trigger's precondition.

**Loose coupling — the seam is an event bus + an action interface, not a hardcoded engine:**

```go
// The Runner (and other writers) emit typed events after a cell changes.
type CoverageEvent struct {
    HostID uint; Source string; Category IntegrationCategory
    Old, New IntegrationState; At time.Time
}

// An automation engine is a community plugin: it subscribes, applies its own View gating, and acts.
type AutomationEngine interface {
    Name() string
    HandleEvent(ctx context.Context, ev CoverageEvent) error
}
```

Engines register like host-status providers. Built-in engine = View-gated Triggers with actions
(create Fleet activity/alert, run a script, call a plugin `Applier` to auto-repair — e.g. Action1
redeploy on `patching→not_installed`). **Zapier/n8n engines** = thin `AutomationEngine`s that POST the
event to an outbound webhook (Zapier catch hook / n8n webhook node), letting the low-code tool own the
View/Trigger logic. Because the contract is just `HandleEvent`, we can run the built-in engine **and**
n8n **and** Zapier simultaneously, or swap them per instance/client — no core change. Delivery is
at-least-once with idempotency keys; a slow/broken engine is isolated (same blast-radius rule as §8).

*This is where Action1's "alert if no successes recently / repair if needed" lives:* the Collector emits
the `at_risk` state + detail (§built); a Trigger gated by a "patching at_risk > 24h" View fires the alert
and/or the auto-repair `Applier`.

## 11. Client/site organization (team/subteam substrate)

The bundle hierarchy (§7) needs a home for "Client" and "Site". Fleet teams are **flat** (no nesting), so:

- **Model:** an `Org` tree fork-side — `Client` (has many `Site`s), each `Site` maps 1:1 to a **Fleet team**
  (the execution substrate we already have). A `Client` is a grouping + the mid-level bundle node; it is
  **not** itself a team. Hosts live in a Site→team.
- **Why not real subteams in Fleet core?** True nested teams touch authz, GitOps, and the team model
  broadly — high-risk, low merge-odds upstream. Mapping Site→team keeps us on Fleet's supported substrate
  and lets bundles **compile down** (§7.4) to per-team config. We get hierarchy + inheritance without
  forking the team model.
- **Authz/tenancy:** a Client scopes its Sites; a Client-admin role sees only its Sites' teams. Cross-client
  references rejected at resolve. Reuses Fleet's team-based authorization underneath.
- **Upstream angle:** propose an optional `team.parent_id` (nullable) as the *minimal* core primitive that
  would let this be native; if declined, the fork-side `Org` tree stands on its own. Pitch small.

## 12. Dashboards / matrixes as plugins

Beyond the built-in coverage matrix, per-client/per-instance **custom dashboards** are themselves plugins,
so we can ship bespoke boards without core changes:

```go
type DashboardPlugin interface {
    Key() string                              // "coverage-matrix", "acme-exec-summary"
    Definition(ctx context.Context) (DashboardDef, error) // panels, each backed by a View (§10) + a viz kind
}
```

A `DashboardDef` is data (panels = View + visualization kind: matrix / count / list / timeseries), rendered
by a generic frontend renderer — so a new board is a registered plugin + a View, not a React deploy. This
composes with §10 (panels are Views) and §11 (a board can be scoped per Client/Site). The built-in coverage
matrix is just the first `DashboardPlugin`. Custom per-client boards and Zapier/n8n-driven ones layer on
without touching core.
