# PLUGINS.md — An extension/provider architecture for our FleetDM fork

> Companion to [`OSS.md`](./OSS.md). Where OSS.md decides *what* we build and *where the code lives* (§9), this document decides *how our differentiators plug in* without forking-in-place across the whole tree. Read [`OSS.md §9`](./OSS.md#9-where-integration-code-should-live) first: it already rules that **integration code lives in first-class MIT packages under `server/` — not a `plugins/` folder — because Fleet has no runtime plugin architecture.** Nothing here overturns that. What follows is the *compile-time* seam design that makes those packages pluggable behind stable interfaces.

## 0. TL;DR / recommendation

- **Do not build a dynamic (runtime-loaded) plugin system.** Go's `plugin` stdlib is a dead end (no Windows, exact-toolchain/dep lockstep, no unload — its own docs steer you to the compile-time approach). `hashicorp/go-plugin` (out-of-process gRPC) is proven but is aimed at *untrusted, third-party, multi-language* plugins you don't control — the opposite of a single-vendor fork where we build both sides.
- **Build a compile-time provider registry** (Go interfaces + `init()`/explicit registration + build tags), the same pattern Fleet *already uses internally* (`hostDetailQueries` map, `EnterpriseOverrides`, the `[]HandlerRoutesFunc` slice) and the same pattern Go's stdlib recommends (`database/sql` driver registration). "Plugin" here means an internal extension seam, compiled in — the code still lives in `server/integrations/*`, `server/endpointprotection/`, `server/mdm/mosyle/` per OSS.md §9.
- **Keep an out-of-process (gRPC) escape hatch** as an *optional future* only for one narrow case: a customer-authored or crash-prone integration we don't trust in-process. Not now.
- **Introduce four provider interfaces:** `HostStatusProvider` (coverage-matrix backbone, generalizes the Munki/MacAdmins pattern), `IntegrationProvider` (config schema + connection test + poller), `RouteRegistrar`, `CronRegistrar`, plus a `DashboardCardProvider` descriptor and a frontend status-column registry.
- **Upstream candidate:** only the thin `HostStatusProvider` / "host integration status" interface. Honest merge probability: **low** (Fleet has no plugin stance and gates large contributions); **low-to-medium** if pitched as a concrete Fleet feature (vendor-agnostic third-party AV/EDR status, "Munki but generic") with a real first-party provider, tests, docs, and an RFC first — *not* as "a plugin system."

Confidence: **high** on the codebase seams (all verified below with file:line); **high** on the Go-plugin tradeoffs (primary docs); **medium-high** on the upstream read (negative web finding — Fleet publishes no plugin stance).

---

## 1. Go plugin approaches for this codebase

### 1.1 Go `plugin` stdlib (`.so`) — unsuitable, documented

The official `pkg.go.dev/plugin` docs are unusually blunt, and every point is disqualifying for a product we ship to Windows/macOS/Linux customers:

- **Platform**: "Plugins are currently supported only on Linux, FreeBSD, and macOS, making them unsuitable for applications intended to be portable." Fleet manages and runs on Windows — **no Windows support is fatal on its own.**
- **Toolchain lockstep**: "Runtime crashes are likely to occur unless all parts of the program (the application and all its plugins) are compiled using exactly the same version of the toolchain, the same build tags, and the same values of certain flags and environment variables."
- **Dependency lockstep**: "Similar crashing problems are likely to arise unless all common dependencies... are built from exactly the same source code." Fleet's `go.mod` is enormous; any drift panics at load (`golang/go#27751`).
- **No unload**: "A plugin is only initialized once, and cannot be closed."
- **The docs' own conclusion** recommends *exactly* the compile-time registry: "...it may be simpler for that person or component to generate Go source files that blank-import the desired set of plugins and then compile a static executable in the usual way."

Verdict: **do not use.** (Confidence: high — primary source.)

### 1.2 Compile-time registry (interface + registration + build tags) — the pragmatic choice

The `database/sql` pattern: a driver's `init()` calls `sql.Register("name", drv)` into a global map; consumers blank-import (`import _ ".../go-sqlite3"`) purely for the side effect, then look the name up. Same idea in `image.RegisterFormat`, `hash`, `crypto`.

Why it fits *this* fork:
- One static binary — no toolchain/dep matching, no IPC, no subprocess supervision, no new failure mode.
- Providers are type-checked and tested *with* the host at compile time.
- Build tags include/exclude features per build (e.g. a customer build without Bitdefender).
- The only cost — "recompile to add a provider" — is a non-cost when we own the build and ship one binary.
- **Fleet itself already works this way** (§2). We're generalizing an existing house pattern, not importing a foreign one.

Verdict: **primary recommendation.** (Confidence: high.)

### 1.3 `hashicorp/go-plugin` (out-of-process, gRPC/net-rpc) — worth it only later, narrowly

Solves the stdlib's problems by running each plugin as a **separate subprocess** over RPC: no shared toolchain/deps, and "a panic in a plugin doesn't panic the plugin user." Powers Terraform providers, Vault, Nomad, Boundary. Its sweet spot is precisely *third-party, untrusted, possibly other-language* plugins with independent release cycles — Terraform's whole model.

But the costs are real and mostly wasted on a single-vendor monolith: ship/version/supervise separate binaries; every call pays protobuf + IPC; you own spawn/health-check/restart/kill; and you must design + version a wire protocol up front. We control both sides of the build, so we get isolation we don't need at a complexity price we'd rather not pay.

**When it becomes worth it (future):** a customer- or partner-authored integration we won't compile into our trusted binary, or an integration prone to crashing/hanging that we want fully isolated from the server process. Design the in-process interfaces (§3) so a gRPC transport can back them later without changing call sites — i.e. keep the interface, swap the implementation. (Confidence: high on the tradeoff; the "later" is a judgment call.)

### 1.4 Recommended: hybrid, compile-time now

Compile-time provider registry now; keep interfaces transport-agnostic so a `go-plugin` gRPC backing is a drop-in for the one-off untrusted case later. This mirrors how Grafana and Terraform actually split things: **frontend plugins compile-in / load as JS bundles; backend plugins that need isolation go out-of-process** — but only when isolation is the actual requirement.

---

## 2. Existing Fleet seams that are already proto-plugin points

Fleet is not plugin-oriented, but it is full of **registration seams** we can generalize. Each below is a place where "add a capability" already means "add an entry," not "edit a monolith."

### 2.1 `EnterpriseOverrides` — function-pointer injection
`server/fleet/service.go:17-44` defines `type EnterpriseOverrides struct{ ... func(...) ... }`, and `service.go:141-145` exposes `SetEnterpriseOverrides(overrides EnterpriseOverrides)` on the `Service` interface. The core service calls these pointers when a premium code path is reached; if `ee/` isn't wired, they're nil/no-op. The struct's own comment — `// TODO: find if there's a better way to accomplish this and standardize.` — is an open invitation.
**Generalization:** this is a hand-rolled, single-consumer registry. A `Registry` (§3) with typed provider slices is the "standardized" version the TODO asks for. **Decision-of-record #1 in OSS.md says keep this seam** — so we *extend* it, we don't replace it.

### 2.2 `ee/` embedding/decorator
`ee/server/service/service.go:17-19`: `type Service struct { fleet.Service; ... }` — the enterprise service **embeds** the free service and overrides methods by shadowing; `service.go:91-108` wires the non-shadowable ones via `svc.SetEnterpriseOverrides(...)`. This is a clean decorator: EE = core + overrides.
**Generalization:** the same decorator shape is how a provider layer wraps core. Our registry is the composition root that assembles core → providers, exactly as `NewService` composes core → EE today.

### 2.3 Config-driven integrations
`server/fleet/integrations.go:472-478`: `type Integrations struct { Jira []*JiraIntegration; Zendesk []*ZendeskIntegration; GoogleCalendar []*GoogleCalendarIntegration; ConditionalAccessEnabled ... }`, embedded in AppConfig at `server/fleet/app.go:774` (`Integrations Integrations` `json:"integrations"`). Adding a first-party integration today means **adding a typed field here** — a closed set, edited in core.
**Generalization:** `IntegrationProvider` (§3) turns this closed struct into an open registry keyed by `Key()`, with a per-provider JSON config schema stored under `integrations.<key>` instead of a new hard-coded Go field each time.

### 2.4 `externalsvc/` external clients — the connection-test pattern
`server/service/externalsvc/jira.go`: `JiraOptions` (`:27`), `NewJiraClient(opts)` (`:36`), and crucially `GetProject` (`:58`) — documented as "used to test in one request the authentication and connection parameters." Zendesk mirrors this (`zendesk.go`). The package doc (`externalsvc.go:1-3`) scopes it to "communicate with external services... via REST APIs," with shared retry/backoff constants.
**Generalization:** `NewClient(opts)` + `TestConnection()` is *exactly* the `IntegrationProvider.ConfigSchema()/TestConnection()` contract. Our Bitdefender/Huntress/Action1 clients live here (or under `server/integrations/*` per OSS.md §9) and satisfy the provider interface.

### 2.5 Cron registration
`cmd/fleet/cron_registration.go`: `cronSchedulesDeps.register(failMsg, newSchedule)` (`:57-64`) wraps `cronSchedules.StartCronSchedule(...)`; `startCronSchedules` (`:69-78`) calls domain-grouped registrars (`registerVulnerabilityCrons`, `registerMDMCrons`, `registerPremiumCrons`, ...). Adding a background job = add a `deps.register(...)` call.
**Generalization:** `CronRegistrar` (§3) lets a provider contribute its poller here. `registerProviderCrons(ctx, deps)` iterates the registry and calls `deps.register` for each — one more line in `startCronSchedules`.

### 2.6 Route registration + the bounded-context `GetRoutes` pattern
Core routes attach in `server/service/handler.go:292 attachFleetAPIRoutes(...)` (called from `:187`). But the **already-generalized seam** is the bounded-context one: `server/mdm/android/service/handler.go:12` `func GetRoutes(fleetSvc, svc) eu.HandlerRoutesFunc`, and the composition point at `cmd/fleet/serve.go:725-726`:
```go
apiHandler = service.MakeHandler(svc, ..., []endpointer.HandlerRoutesFunc{
    android_service.GetRoutes(svc, androidSvc), activityRoutes, acmeRoutes, chartRoutes,
}, extra...)
```
`MakeHandler` takes a **slice of `HandlerRoutesFunc`**. Adding routes = appending to that slice. This is the single cleanest existing plugin seam in the codebase.
**Generalization:** `RouteRegistrar.Routes() endpointer.HandlerRoutesFunc`; the registry contributes `reg.RouteFuncs()...` into that slice literal at `serve.go:725`.

### 2.7 Per-host third-party status — Munki/MacAdmins (THE coverage-matrix precedent)
This is the pattern our coverage matrix reuses, end to end:
- **Types**: `server/fleet/hosts.go:1312 HostMunkiInfo`, `:1489-1493 MacadminsData{ Munki, MDM, MunkiIssues }`, `:1522-1527 AggregatedMDMData`, `:1542-1548 AggregatedMacadminsData` (per-host + fleet-wide aggregate).
- **Ingestion is a registry**: `server/service/osquery_utils/queries.go:183 var hostDetailQueries = map[string]DetailQuery{...}`; the `"munki_info"` entry (`:675-680`) binds a SQL query + `DirectIngestFunc: directIngestMunkiInfo`; `directIngestMunkiInfo` (`:2800-2812`) calls the datastore writer.
- **Datastore writer**: `server/fleet/datastore.go:1234 SetOrUpdateMunkiInfo(ctx, hostID, version, errors, warnings)` (and `:1235 SetOrUpdateMDMData`, `:1248 SetOrUpdateHostDisksEncryption` for BitLocker/FileVault — the exact categories we want in the matrix).
- **Read/aggregate**: `datastore.go:421 GetHostMunkiVersion`, `:423 GetHostMDM`.

The shape is: **normalized per-host rows in a dedicated table, written on an ingestion cadence (`SetOrUpdate*`), read + aggregated for the UI.** Our `host_integration_status` table and `HostStatusProvider` are a direct generalization: same write-on-sync / read-and-aggregate flow, but keyed by `(source, category)` instead of one hard-coded table per vendor.

### 2.8 The datastore interface + generated mocks
`server/fleet/datastore.go` is the `Datastore` interface. Mocks are **generated**: `server/mock/datastore_mock.go:1` `// Automatically generated by mockimpl. DO NOT EDIT!`, produced via `Makefile:456 generate-mock`. Per CLAUDE.md, adding an interface method requires regenerating mocks or `go test ./server/service/` crashes on nil mock funcs.
**Generalization:** our provider datastore methods (`SetOrUpdateHostIntegrationStatus`, `GetHostIntegrationStatus`) are added the same way — extend the interface, `make generate-mock`, run service tests. No new machinery.

### 2.9 Frontend registration points
- **Routes**: `frontend/router/index.tsx:144` route tree; `:109 import PremiumRoutes` — the tier-gating wrapper (`components/PremiumRoutes`) is the analog of "conditionally register this plugin's page."
- **Host table columns**: `frontend/pages/hosts/ManageHostsPage/HostTableConfig.tsx:45 IHostTableColumnConfig = Column<IHost> & {...}`; `:95 allHostTableHeaders(teamId): IHostTableColumnConfig[]` is the **column array** (the registry); `:726 generateAvailableTableHeaders` filters it by tier/role (e.g. it drops `mdm.server_url`, `mdm.enrollment_status` in free tier at `:746-752`); `:768 generateVisibleTableColumns` then removes user-hidden columns.
**Generalization:** a coverage-matrix column is one more entry appended to `allHostTableHeaders`, gated in `generateAvailableTableHeaders`. A frontend status-column registry (§3.5) makes plugins contribute entries instead of editing the literal.

---

## 3. Proposed architecture for our fork

### 3.0 Guiding principles (reconciled with OSS.md §9)
- **Registry, not runtime loader.** Compile-time composition. "Plugin" == provider registered into a central `Registry` the binary assembles at startup.
- **Code location unchanged.** Providers live in `server/integrations/{action1,bitdefender,huntress}`, `server/endpointprotection/`, `server/mdm/mosyle/` (OSS.md §9). The registry package is *wiring only*.
- **Follow house conventions**: `ctxerr.Wrap(ctx, err, "...")`, slog `*Context` methods, `new(expr)` not `server/ptr`, `fleet.NewInvalidArgumentError` for config validation, `fleethttp.NewClient()`.
- **Resilience**: one provider failing must never fail the matrix or the request. Read paths read the datastore, not live vendor APIs (mirrors Munki).

### 3.1 `HostStatusProvider` — coverage-matrix backbone
Lives in the new `server/endpointprotection/` bounded context (already named in OSS.md §9). Generalizes §2.7. Providers **write** normalized status on their poll cadence; the matrix **reads** the table.

```go
package endpointprotection

// Category is a normalized coverage dimension shown as a matrix column.
type Category string

const (
	CategoryAV             Category = "av"
	CategoryMDR            Category = "mdr"
	CategoryRemoteAccess   Category = "remote_access"
	CategoryBackups        Category = "backups"
	CategoryDiskEncryption Category = "disk_encryption"
)

// State is the normalized per-cell value (kept small so the UI stays vendor-agnostic).
type State string

const (
	StateProtected    State = "protected"
	StateAtRisk       State = "at_risk"
	StateNotInstalled State = "not_installed"
	StateUnknown      State = "unknown"
)

// HostStatus is one provider's reading for one host and category.
type HostStatus struct {
	HostID      uint
	Category    Category
	Source      string    // stable provider key, e.g. "bitdefender"
	State       State
	Detail      string    // free-form, shown on hover in the matrix
	CollectedAt time.Time
}

// HostStatusProvider is implemented by any integration that feeds the coverage
// matrix. Implementations sync into the datastore (SetOrUpdateHostIntegrationStatus);
// they are NOT called synchronously on the host-read path.
type HostStatusProvider interface {
	Source() string          // matches externalsvc client naming
	Categories() []Category  // which columns this provider owns
}
```

New datastore methods (added to `server/fleet/datastore.go`, mocks regenerated per §2.8), directly modeled on `SetOrUpdateMunkiInfo`:

```go
// SetOrUpdateHostIntegrationStatus upserts one (host, source, category) cell.
SetOrUpdateHostIntegrationStatus(ctx context.Context, s endpointprotection.HostStatus) error
// GetHostIntegrationStatus returns all cells for a host (coverage matrix, per host).
GetHostIntegrationStatus(ctx context.Context, hostID uint) ([]endpointprotection.HostStatus, error)
// AggregatedIntegrationStatus powers fleet-wide dashboards (mirrors AggregatedMacadminsData).
AggregatedIntegrationStatus(ctx context.Context, teamID *uint) ([]endpointprotection.AggregatedStatus, error)
```

Read path (service method, house style):
```go
func (svc *Service) HostCoverageMatrix(ctx context.Context, hostID uint) ([]endpointprotection.HostStatus, error) {
	if err := svc.authz.Authorize(ctx, &fleet.Host{}, fleet.ActionRead); err != nil {
		return nil, err
	}
	host, err := svc.ds.HostLite(ctx, hostID)
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "load host for coverage matrix")
	}
	if err := svc.authz.Authorize(ctx, host, fleet.ActionRead); err != nil {
		return nil, err
	}
	rows, err := svc.ds.GetHostIntegrationStatus(ctx, hostID)
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "get host integration status")
	}
	return rows, nil
}
```

### 3.2 `IntegrationProvider` — config schema + connection test + poller
Generalizes §2.3 (config) + §2.4 (client/connection test) + §2.5 (cron). Providers store settings under `integrations.<key>` as `json.RawMessage`.

```go
type IntegrationProvider interface {
	// Key is the stable identifier; also the AppConfig.Integrations sub-key.
	Key() string
	// ConfigSchema returns a JSON schema so the settings UI renders generically.
	ConfigSchema() json.RawMessage
	// TestConnection validates credentials without side effects — the generalization
	// of externalsvc Jira.GetProject / Zendesk.GetGroup.
	TestConnection(ctx context.Context, cfg json.RawMessage) error
	// Sync runs one poll cycle: pull from the vendor, normalize, and write via
	// SetOrUpdateHostIntegrationStatus. Errors are wrapped, not fatal.
	Sync(ctx context.Context, cfg json.RawMessage) error
	// Interval is the default poll cadence (overridable by config).
	Interval() time.Duration
}
```
Validation follows house style (`fleet.NewInvalidArgumentError("integrations.bitdefender.api_key", "is required")`, accumulate, `HasErrors()`), validated **within the incoming config payload**, not against the DB (per CLAUDE.md guidance for declarative endpoints).

### 3.3 `RouteRegistrar` / `CronRegistrar` — reuse existing shapes
```go
type RouteRegistrar interface {
	Routes() endpointer.HandlerRoutesFunc // appended at serve.go:725 slice literal
}

type CronRegistrar interface {
	// Schedules returns constructors registered via cronSchedulesDeps.register.
	Schedules() []func() (fleet.CronSchedule, error)
}
```
`IntegrationProvider` can satisfy `CronRegistrar` by wrapping `Sync` in a schedule — most integrations only need the poller, not custom routes.

### 3.4 `DashboardCardProvider` — descriptor, not code injection
Backend exposes card *metadata* (id, title, which aggregate to fetch, which frontend widget renders it); the frontend maps ids to React components. Keeps the API/data contract in Go and the rendering in TS, avoiding any "ship JS from the backend" coupling.
```go
type DashboardCardProvider interface {
	Cards(ctx context.Context, teamID *uint) ([]DashboardCard, error) // {ID, Title, Widget, Data}
}
```

### 3.5 The central `Registry` (composition root)
One package (wiring only), assembled at startup — the standardized version of `EnterpriseOverrides`' TODO.

```go
package providerreg

type Registry struct {
	HostStatus   []endpointprotection.HostStatusProvider
	Integrations []IntegrationProvider
	Routes       []RouteRegistrar
	Crons        []CronRegistrar
	Cards        []DashboardCardProvider
}

// init()-registration (database/sql style) OR explicit New() wiring — prefer explicit
// for a single-vendor fork (greppable, testable, no import-ordering surprises).
func Default(ds fleet.Datastore, logger *slog.Logger, cfg config.FleetConfig) *Registry { ... }
```

**Startup wiring (three touch points):**
1. `cmd/fleet/serve.go:725` — append `reg.RouteFuncs()...` into the `[]endpointer.HandlerRoutesFunc` literal (§2.6).
2. `cmd/fleet/cron_registration.go:startCronSchedules` — add `registerProviderCrons(ctx, deps, reg)` that loops `reg.Crons` and calls `deps.register(...)` (§2.5).
3. `ee/`-style service composition — providers that need to serve status reads are reachable via the datastore, so most need no service wiring; those that do get called from a thin core service method (§3.1), keeping the `EnterpriseOverrides` seam intact per OSS.md decision #1.

**Build tags** (`//go:build !nobitdefender` etc.) let a customer build omit providers; a single `providers_wire.go` blank-imports the enabled set — the exact pattern the Go `plugin` docs recommend as the alternative to dynamic loading.

### 3.6 Frontend status-column registry
Turn `allHostTableHeaders` (`HostTableConfig.tsx:95`) into a base array + a registered-column array:
```ts
// frontend/pages/hosts/coverage/statusColumnRegistry.ts
export interface CoverageColumn {
  id: string;                       // e.g. "coverage.av"
  category: "av" | "mdr" | "remote_access" | "backups" | "disk_encryption";
  Header: string;
  gate?: (ctx: { isPremiumTier: boolean; config: IConfig }) => boolean;
  Cell: (host: IHost) => JSX.Element; // renders the status icon + tooltip
}
export const coverageColumns: CoverageColumn[] = [];
export const registerCoverageColumn = (c: CoverageColumn) => coverageColumns.push(c);
```
`allHostTableHeaders` spreads `coverageColumns` (filtered by `gate`) into its return; `generateAvailableTableHeaders` (`:726`) keeps doing the tier/role gating it already does. Each provider's frontend module registers its column at import time — the JS-bundle equivalent of the Go `init()` registry.

---

## 4. Upstreamability

### 4.1 Fleet's stated posture (evidence)
No official plugin/extension architecture exists for the server, and no public issue requests one — a **negative finding across multiple official pages** (fleetdm.com/integrations, /guides/automations, /handbook). Fleet's model is **integrations, not plugins**: outbound webhooks + REST API + `fleetctl` + a *fixed first-party set* (Jira, Zendesk, Google Calendar, etc.). The only "extension" concept Fleet ships is **agent-side osquery extensions** delivered via TUF — not server plugins. The handbook's "Why this way?" explicitly favors a monolith ("avoid preemptive structure, choose 'boring' solutions, reuse systems," "one repo is easier to manage"). And contributions that "change the user interface, the CLI usage, or the REST API... must go through drafting and reconsideration before merging" — large feature drops are **not** auto-accepted. (Confidence: medium-high; the "no plugin issue exists" is a web-search negative, not an exhaustive tracker audit.)

Implication: pitching "a plugin system" upstream is a near-certain non-starter — it's speculative abstraction against a deliberately monolithic project.

### 4.2 The minimal upstreamable slice
**`HostStatusProvider` / a generic "host integration status" interface that generalizes Munki/MacAdmins (§2.7).** It's the best candidate because:
- It **fills a gap Fleet already has**: today AV/EDR/backup/remote-access coverage isn't representable; Munki and MDM each got a bespoke table. A vendor-agnostic `host_integration_status` table + `SetOrUpdate/Get` pair is "Munki, but generic" — a shape Fleet's own patterns already endorse.
- **Low blast radius**: additive interface + one table + read endpoint; backward-compatible; no change to existing flows.
- It reads as *a feature*, not *an architecture*: "third-party protection status on the host details page," which maps to Fleet's existing "MacAdmins" card.

### 4.3 Honest merge probability
- As "a plugin system": **low** (near zero).
- As "generic host integration status, with a real first-party provider wired through it, tests, docs, RFC-first": **low-to-medium**.

What maximizes it: (1) **RFC/issue first** — Fleet requires drafting for API/UI changes; open a feature request describing the user problem (unified coverage view), not the abstraction. (2) **Ship a concrete first-party consumer** — don't submit a bare interface; wire one existing signal (e.g. disk-encryption/BitLocker status, already at `datastore.go:1248`) or a genuinely useful public source through it so it's not speculative. (3) **Small, additive, backward-compatible**, following the exact Munki table/`SetOrUpdate`/aggregate shape. (4) **Tests + docs + an activity/audit-log entry** if it writes state (`docs/Contributing/reference/audit-logs.md`). (5) Frame it in **their terminology** ("integration status," matching the MacAdmins card), not "provider/plugin."

### 4.4 The split
- **Upstream (thin):** the `HostStatusProvider`-style interface + `host_integration_status` table + read endpoint, and *maybe* one public provider. That's it.
- **Stays in our fork (everything else):** the full `Registry`, `IntegrationProvider` (config schema + connection test + poller), `RouteRegistrar`/`CronRegistrar`, `DashboardCardProvider`, the coverage-matrix UI, and all vendor providers (Bitdefender, Huntress, Action1, ScreenConnect, Mosyle). These are our differentiators and align with OSS.md §9 (MIT packages under `server/`). Keeping the *full* system in-fork also sidesteps the rebase risk of upstream never accepting the broad seam.

---

## 5. How comparable OSS tools do it (lessons)

- **osquery (most relevant — Fleet *is* an osquery manager).** Extensions are **separate processes** speaking **Thrift** (not gRPC) over a Unix socket / named pipe; SDKs in C++, Go (`osquery-go`), Python. Chosen for **language independence, independent/proprietary builds, and privilege separation** ("a non-privileged extension cannot register plugins to a root osqueryd"; the agent refuses world-writable extension binaries). Lifecycle: broadcast-register plugins, `ping`/`call` health with latency-based **deregistration**, `--extensions_autoload`. **Lessons:** a *versioned IDL* decouples release cycles; *health-based deregistration* keeps a hung plugin from stalling the host; *load-time security checks* (ownership/permissions, privilege separation) matter; one plugin abstraction serves multiple capability types (table/config/logger). Sources: osquery.readthedocs.io "osquery SDK" and "Using Extensions."
- **Terraform providers (`hashicorp/go-plugin`, gRPC).** Separate provider binaries; **handshake (magic cookie + protocol version)**; numbered protocols (5/6) with mux/translation adapters so old and new providers coexist while core codes to one `providers.Interface`; registry advertises compatible protocol versions; RPC panics don't crash core; optional TLS. **Lessons:** *version the protocol from day one and keep core version-agnostic*; a handshake prevents a stray binary from misbehaving.
- **Grafana.** Split: **frontend** panel/datasource plugins are JS bundles loaded at runtime via SystemJS with a **shared-dependency import map** (plugins don't re-bundle React); **backend** datasource plugins are Go subprocesses over go-plugin/gRPC with an SDK that handles instance lifecycle. **Signed plugins by default** (`MANIFEST.txt` SHA256s verified against a built-in key; private/community/commercial tiers). **Lessons:** *frontend plugins compile-in or load as bundles, never subprocesses*; *sign/verify third-party code*; a stable shared-dep contract is mandatory if you load bundles at runtime.
- **Backstage.** Plugins are **npm packages** composed largely at **build time**; frontend uses `createFrontendPlugin` + extension points; the new backend system runs plugins **in-process** with dependency-injected services and "no direct code calls between plugins." **Lessons:** *build-time composition is simplest and type-safe* (our exact stance); *extension points / DI* give structure without a runtime loader; in-process is simpler but couples faults/deps (a real tradeoff vs. Grafana/Terraform's isolation).

Net: everyone puts **frontend plugins in the browser as bundles/compiled-in**, and reserves **out-of-process gRPC for backend plugins that need isolation or third-party/multi-language authorship**. For a single-vendor Go+React fork, that validates: **compile-in both sides now; keep gRPC in reserve for the untrusted case.**

---

## 6. Risks & tradeoffs

- **ABI / stability.** Compile-time registry has *no ABI problem* — everything is one binary, type-checked together. The tradeoff is "recompile to add a provider," which is free for us. (If we ever add gRPC providers, we inherit protocol-versioning obligations — design the interfaces transport-neutral now so that's a later, contained decision.)
- **Security — the server has admin on everything.** A compiled-in provider runs **with full server privileges** and can touch any datastore method; there is *no sandbox*. Mitigations: (1) providers get **narrow, purpose-built datastore methods** (`SetOrUpdateHostIntegrationStatus`), not the whole `Datastore`; (2) all provider service entry points keep the **double-authorize** pattern (`authz.Authorize` generic then entity-scoped); (3) credentials validated + stored per house style, never logged; (4) treat any *future* third-party/untrusted provider as the trigger to move it **out-of-process** (go-plugin), which is precisely osquery's privilege-separation lesson. Route auth flows through the existing `server/authz/policy.rego`; a coverage endpoint must not become an unauthenticated data leak.
- **Performance.** Read paths **must read the `host_integration_status` table, not call vendor APIs synchronously** (the Munki lesson) — otherwise the host list blocks on Bitdefender's rate limits. Provider `Sync` runs on crons with backoff (reuse `externalsvc` retry constants). A slow/failing provider degrades one column to `unknown`, never the page. Watch vendor rate limits at fleet scale (already flagged in OSS.md §11).
- **`ee/` clean-room boundary.** Per `clean-room-protocol.md` and OSS.md decision #6, provider code and the registry are **greenfield MIT** — implementers must not read `ee/` source. The registry *extends* the public `fleet.Service`/`EnterpriseOverrides` seams (whose signatures are echoed in MIT core), so it needs no `ee/` knowledge. Keep the tiering machinery intact (decision #1): a provider can be premium-gated via the existing `license.IsPremium(ctx)` checks without inlining premium logic.
- **Rebase / upgrade.** The three startup touch points (§3.5) are **small, localized diffs** in `serve.go`, `cron_registration.go`, and the `HostTableConfig` array — low rebase conflict surface, far better than scattering provider calls across core. If the thin `HostStatusProvider` slice merges upstream, our rebases shrink further; if it doesn't, we've lost nothing because the full system was always fork-local. The biggest rebase risk is upstream refactoring the very seams we hook (`hostDetailQueries`, the `HandlerRoutesFunc` slice, `EnterpriseOverrides`) — mitigated by keeping our additions *additive* and each behind one small adapter.

---

## 7. Recommended path (phased)

1. **Phase A — registry + `HostStatusProvider` + coverage matrix (our top differentiator).** New `server/endpointprotection/` bounded context; `host_integration_status` table + `SetOrUpdate/Get/Aggregated` datastore methods (regenerate mocks); read endpoint + frontend status-column registry. Wire the first real provider (Bitdefender or BitLocker/disk-encryption reusing `datastore.go:1248`). This proves the whole pattern end-to-end on the feature we care about most.
2. **Phase B — `IntegrationProvider` (config schema + `TestConnection` + poller) + `CronRegistrar`.** Migrate Bitdefender/Huntress/Action1/Mosyle clients (living in `server/integrations/*`, `server/mdm/mosyle/` per OSS.md §9) onto the interface; wire crons via `startCronSchedules`.
3. **Phase C — `RouteRegistrar` + `DashboardCardProvider`** for provider-specific pages/cards, appended at the `serve.go:725` slice and the dashboard.
4. **Phase D (optional, upstream).** Open an RFC/issue for a generic "host integration status" interface + table, backward-compatible, with one first-party provider, tests, and docs. Expect **low-to-medium** odds; lose nothing if declined.
5. **Phase E (deferred, only if needed).** Out-of-process gRPC (`hashicorp/go-plugin`) backing for a single untrusted/crash-prone integration — interfaces from Phases A-C already make this a transport swap, not a redesign.

---

### Key file references
- Go-plugin seams: `server/fleet/service.go:17-44,141-145`; `ee/server/service/service.go:17-19,91-108`
- Config-driven integrations: `server/fleet/integrations.go:472-478`; `server/fleet/app.go:774`
- External client + connection test: `server/service/externalsvc/jira.go:27,36,58`
- Cron registry: `cmd/fleet/cron_registration.go:57-64,69-78`
- Route registry: `server/service/handler.go:187,292`; `server/mdm/android/service/handler.go:12-20`; `cmd/fleet/serve.go:725-726`
- Per-host status precedent: `server/fleet/hosts.go:1312,1489-1493,1522-1548`; `server/service/osquery_utils/queries.go:183,675-680,2800-2812`; `server/fleet/datastore.go:1234-1248,421-423`
- Datastore mocks: `server/mock/datastore_mock.go:1`; `Makefile:456`
- Frontend registries: `frontend/router/index.tsx:109,144`; `frontend/pages/hosts/ManageHostsPage/HostTableConfig.tsx:45,95,726,768`
- Fork context to honor: `human/OSS.md` §9 (code location), Decisions of record #1 (keep `EnterpriseOverrides`/tiering) and #6 (clean-room)
