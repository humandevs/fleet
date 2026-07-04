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

| Piece | Home | Rationale |
|---|---|---|
| `host_integration_status` table + types + read API | **Upstream (MIT)** | Generic "Munki, but for any coverage vendor". *Merged.* |
| `HostStatusProvider` / `Collector` interface + `Runner` | **Upstream (MIT)** | Vendor-agnostic ingestion seam; no vendor code. |
| Coverage-matrix dashboard + `ColumnSpec` (icons/labels) | **Upstream (MIT)** | Renders normalized cells; no vendor knowledge. |
| Status filtering (host list by coverage) | **Upstream (MIT)** | Query over the generic table. |
| Config-inheritance resolver (merge global→client→site) | **Upstream candidate (MIT)** | Generic tree-merge; useful beyond us. Pitch carefully — may land fork-side first. |
| ScreenConnect / Bitdefender / Action1 / Veeam / iDrive360 plugins | **Fork (Apache-2.0)** | Vendor-specific; our contribution to *demonstrate* the seams. |
| *Standard MSP* bundle catalog + policies | **Fork (private)** | Our commercial packaging. |

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

**Filter model:**

```go
type CoverageFilter struct {
    // Any of these categories in a non-protected state (staleness applied) → "problem".
    ProblemCategories []IntegrationCategory
    // Exact (category,state) predicates, AND-combined with the above when both set.
    Predicates []struct{ Category IntegrationCategory; State IntegrationState }
    // MissingCategories: host is missing coverage (no protected cell) for ALL listed categories.
    MissingCategories []IntegrationCategory
}
```

**SQL shape (anti-join / EXISTS, staleness-aware):** a host matches "missing AV" when **no** fresh
`host_integration_status` row exists with `category='av' AND state='protected' AND updated_at > now - ttl`.
"Problems" = `EXISTS` a fresh row in (`at_risk`,`not_installed`) **or** `NOT EXISTS` a fresh `protected`
row for an expected category. Freshness must be applied **in SQL** (mirror the read-path TTL) so a stale
"protected" doesn't hide a real gap — this is the one subtlety; see
[RISK-REGISTER.md](./RISK-REGISTER.md) #3. Expected categories come from the host's **bundle** (§7): a host
whose bundle includes Managed AV but has no fresh protected AV cell is the canonical "missing AV" row.

*Implementation note:* new datastore method `ListHostsByCoverage(ctx, filter, opts)` + host-list wiring +
mock regen. Order-key allowlist for sorting. Staged (needs `MYSQL_TEST` round-trip).

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
| 4 | Bitdefender GravityZone `Collect` (`av`+`mdr`) | ⏳ next |
| 5 | Action1 (patching) + Veeam/iDrive360 (`backups`) | ⏳ next |
| 6 | `ColumnSpec` + frontend coverage matrix | ⏳ next |
| 7 | `ListHostsByCoverage` + host-list filter wiring (+ mock regen) | ⏳ next |
| 8 | `ResolveBundle` resolver (pure) + tests | ⏳ next |
| 9 | Bundle → Fleet team/GitOps compiler + `Applier` seam | ⏳ later |

**PR sequencing (upstream):** land #1 (drafted) → #2 (interface+runner, no vendor code) → #6 dashboard →
#7 filtering → optionally #8 resolver. Vendor plugins (#3–#5) ship on our GitHub, not upstream. Each
upstream PR is framed as a user-facing feature with one real consumer + tests — never as "a plugin system"
([PLUGINS.md §4.3](./PLUGINS.md)).
