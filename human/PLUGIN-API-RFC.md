# RFC: A stable, versioned plugin API for FleetDM server extensions

> Status: **Draft RFC** (the artifact we'd open as an issue/RFC on Fleet, and the design of record for our
> fork). Supersedes the *distributable-plugin* portions of [`PLUGINS.md`](./PLUGINS.md) (§1.3, §3.5, §7
> Phase E); the in-process first-party registry in PLUGINS.md still stands for code we compile ourselves.
> Companions: [`OSS.md §9`](./OSS.md#9-where-integration-code-should-live), [`MVP.md`](./MVP.md).

## 1. Problem & goal

We're building UEM features on a FleetDM fork as **plugins** (coverage-matrix collectors for AV/MDR/
backups/remote-access, third-party integrations, dashboards). The hard requirement:

> **Plugins must keep working across minor upstream Fleet upgrades without being rewritten.** We upgrade
> the base Fleet package; our compiled plugins keep running. Occasional deliberate plugin-API version
> bumps are acceptable; the day-to-day surface must be **as small and stable as possible**, and plugins
> must be **compiled and distributed independently of Fleet core** (so core can update freely, and third
> parties can ship plugins).

**Why in-process compile-time plugins can't satisfy this:** Go has no stable ABI. An in-process plugin
recompiles against each Fleet build, and any change to Fleet's internal Go types ripples into it. The
`.so` `plugin` stdlib is worse (no Windows, exact toolchain+dependency lockstep). **Version-portability
requires a wire boundary** — a stable, versioned API the plugin depends on *instead of* Fleet's internals.
This is your "internal app-only API," and it's the correct instinct.

## 2. The core design: a versioned Plugin API + an in-core version firewall

```
   compiled plugin (separate artifact, NOT in core; WASM module or gRPC binary)
        │   depends ONLY on the semver'd Plugin API contract (never on github.com/fleetdm/fleet/...)
        ▼   gRPC over local socket  /  WASM host-function imports
   ┌──────────────────────────────────────────────────────────────┐
   │  Fleet core                                                    │
   │    Plugin Host  ──────────────  STABLE, semver'd, TINY surface │  ← the "app-only API"
   │        │                                                       │
   │    Adapter / shim  ─────────────  absorbs Fleet's churn        │  ← THE VERSION FIREWALL
   │        │                                                       │
   │    Fleet internal Service / Datastore  ── changes every minor  │
   └──────────────────────────────────────────────────────────────┘
```

- The plugin speaks **only the versioned Plugin API** — a curated, minimal contract. It never imports
  Fleet's Go packages, so Fleet's internal churn is invisible to it.
- Inside core, a thin **adapter** maps the stable API onto Fleet's *current* internal interfaces. When a
  minor Fleet release changes internals, **only the adapter changes — the API contract and your compiled
  plugins do not.** That is literally the "upgrade core, plugins keep working" property.
- Breaking the contract = a rare, deliberate **Plugin-API major bump**, versioned **independently of
  Fleet's own version** (your "occasional plugin version bumps").

## 3. Scope: what is (and isn't) a portable plugin

Not everything can cross a stable wire boundary. Two tiers, and being honest about the line is the whole
design:

### Tier 1 — the portable **data-plane** API (the small, stable surface) — **primary**
Flat, serializable operations. This is where version-portability is clean and where **most of our
third-party work lives**:
- **`WriteHostStatus(source, category, host_id, state, detail, collected_at)`** → `SetOrUpdateHostIntegrationStatus` (the coverage matrix). *Scoped:* a plugin may only write its **own** `source`/`category`.
- **`GetConfig(plugin_key)`** → the plugin's slice of `AppConfig.Integrations` (secrets injected, never returned to logs).
- **`HTTPRequest(url, ...)`** → host-mediated outbound call, **gated by a per-plugin `allowed_hosts` allow-list** (so a collector can reach *its* vendor API and nothing else).
- **Lifecycle callbacks the host invokes on the plugin:** `Sync()` (poll tick), `HandleWebhook(body)` (push events), `TestConnection(config)`, `Describe()` (declares `source`, `categories`, config schema, poll interval).

This maps 1:1 onto the `HostStatusProvider` / `IntegrationProvider` interfaces already in
[PLUGINS.md §3](./PLUGINS.md#3-proposed-architecture-for-our-fork). It's exactly the shape go-plugin/
Terraform providers and Extism plugins already prove works across versions.

### Tier 2 — **control-plane** hooks that stay **in-process / first-party**
Routes, cron registration, dashboard cards, host-actions, policy-remediation, notification channels
traffic in **non-serializable live Go types** (`http.ResponseWriter`/`*http.Request`, cron closures,
in-memory UI structs, the `Datastore`). A wire adapter for these is lossy and complex, so they **stay
compiled-in as first-party providers** (PLUGINS.md compile-time registry), transport-neutral so a *narrow
serializable subset* could go out-of-process later where it makes sense (e.g. a webhook **body parser**, a
remediation **decision function**). We tell partners plainly: **portable compiled plugins = collectors/
integrations; deep server extensions = first-party.**

> This directly answers "can all our work move to plugins?": **the integration/collector work — yes,
> portably; the deep server-internals hooks — first-party in-process.** The small stable surface is a
> feature, not a limitation.

## 4. Transport

One transport-neutral Go interface set for Tier 1; the host picks a backing per plugin:

| Backing | For | Why |
|---|---|---|
| **In-process registry** | First-party collectors we build & trust (Bitdefender/Huntress/Action1/Mosyle) | Native speed, one binary, no IPC. Default. |
| **WASM (Extism + wazero)** | **Primary** distributable third-party collectors | ONE signed cross-platform `.wasm` (incl. Windows, via wazero's pure-Go/no-CGO runtime); **capability sandbox** (no ambient FS/network) — the best fit when the server has admin over every device. |
| **gRPC (hashicorp/go-plugin)** | Escape hatch: plugins needing native OS access/CGO or a heavy non-WASM ecosystem | Terraform/Vault model: per-OS subprocess binaries, versioned protocol handshake, crash isolation, checksum+TLS. Heavier ops; OS-confine the subprocess. |

Adapters (`wasmHostStatusProvider`, `grpcHostStatusProvider`) each satisfy the same Go interface and drive
the same `SetOrUpdateHostIntegrationStatus` write path — **adding a transport is a new adapter, not a new
call site.** osquery's Thrift extension SDK is the **design precedent** (Fleet already manages out-of-
process extensions on the agent), not a transport we adopt for a new seam.

## 5. Security model (the server has admin over every device)

A malicious plugin is a fleet-wide compromise, so:

1. **Signing & verification** (Grafana model): no distributable plugin loads without a valid signature.
   Sign artifacts (cosign/sigstore or a fork key), pin trusted keys in config, verify hash+signature at
   load, **refuse unsigned/modified**, record `plugin_id + hash + signer` in the audit log. Tier trust:
   first-party (in-tree) / partner-signed / customer-private.
2. **Sandbox — strong, not absolute.** WASM is capability-default-deny (no ambient FS/network; WASI opt-in;
   HTTP host-controlled via `allowed_hosts`). But **the effective trust boundary is the host-function
   surface, not the sandbox** — treat the sandbox as defense-in-depth backed by signing, and (for gRPC)
   OS-confine the subprocess (separate low-priv uid, job object/seccomp). Note wazero is young (1.0 =
   Mar 2023): runtime-escape is a residual risk against a high-value target.
3. **Least-privilege host API.** Never hand a plugin the `Datastore`. Expose only the narrow Tier-1
   functions. **Critically (data-poisoning defense):** the host enforces that a plugin may write status
   **only for its own `source`/`category`** and **cannot fabricate `host_id`s** it wasn't given — the
   sandbox does nothing about *semantic* abuse of a granted capability, so this is server-side validation,
   not a plugin promise. Egress scoped to `allowed_hosts`.
4. **Supervision.** Runtime CPU/mem/wall-clock limits + health/timeout so a slow/failing plugin degrades
   **one coverage-matrix cell to `unknown`, never the request** (the Munki read-path lesson). Load-time
   ownership/permission checks on on-disk artifacts (osquery's lesson: refuse world-writable binaries).

## 6. Versioning & compatibility policy

- The Plugin API is a **semver'd protobuf** (`plugin/v1/*.proto`). Backward-compatible evolution = add
  fields/messages/RPCs; never renumber/remove within a major.
- A plugin declares the **min Plugin-API version** it needs; the host advertises what it supports and
  refuses incompatible plugins with a clear error (go-plugin's `ProtocolVersion` handshake; a version
  field in the WASM `Describe()`).
- **Plugin-API version is independent of Fleet's version.** Fleet minor upgrades bump the adapter, not the
  API → plugins keep working. Only a deliberate Plugin-API major bump requires plugin updates.
- Auth: a plugin authenticates to the Host API as an **api-only service identity** (reuse Fleet's existing
  `api_only` user/token concept) — so the "app-only API" even reuses an existing auth surface.

## 7. Upstream track vs. fork track (honest)

- **Upstream (thin, feature-framed — LOW-to-MEDIUM odds):** only the `HostStatusProvider`-style **"host
  integration status"** interface + one additive `host_integration_status` table + `SetOrUpdate/Get/
  Aggregated` datastore methods + a read endpoint, wired to **one real first-party consumer** (reuse the
  existing BitLocker/FileVault signal at `datastore.go:1248`). Pitch as a *feature* ("unified third-party
  coverage on the host page, like the MacAdmins card"), in Fleet's terminology, **never as "a plugin
  system."** RFC/issue first (Fleet requires drafting). Optional second slice: a registration hook on the
  already-internal `hostDetailQueries` map (`osquery_utils/queries.go:183`).
- **Fork-side (permanently — near-zero upstream odds):** the **loader itself** (WASM/gRPC host), the
  central Registry, config-schema/TestConnection, route/webhook/dashboard/host-action/remediation/
  notification providers. A runtime plugin loader is exactly the speculative abstraction a deliberately-
  monolithic Fleet avoids. **Architect so upstream rejection costs nothing** — every seam is an additive
  one-adapter diff at the three composition points (`serve.go:725` route slice,
  `cron_registration.go startCronSchedules`, the `HostTableConfig` array), keeping the full system fork-
  local and rebase-cheap. The osquery-extension precedent helps the *pitch* but won't move the loader
  question.

## 8. Reference proto sketch (Tier 1)

```proto
syntax = "proto3";
package fleetplugin.v1;

// Host API — what Fleet core exposes to plugins (least-privilege; NO Datastore).
service PluginHost {
  rpc WriteHostStatus(WriteHostStatusRequest) returns (WriteHostStatusResponse); // scoped to caller's source
  rpc GetConfig(GetConfigRequest) returns (GetConfigResponse);                   // this plugin's config slice
  rpc HTTPRequest(HTTPRequestRequest) returns (HTTPRequestResponse);             // gated by allowed_hosts
  rpc EmitActivity(EmitActivityRequest) returns (EmitActivityResponse);          // audit log
}

// Plugin API — what a plugin exposes to core.
service Plugin {
  rpc Describe(DescribeRequest) returns (DescribeResponse);       // source, categories, config schema, interval, min_api_version
  rpc TestConnection(TestConnectionRequest) returns (TestConnectionResponse);
  rpc Sync(SyncRequest) returns (SyncResponse);                   // one poll cycle
  rpc HandleWebhook(HandleWebhookRequest) returns (HandleWebhookResponse); // push events (body only)
}

message HostStatus {
  string source = 1;           // MUST equal the plugin's declared source (host-enforced)
  string category = 2;         // av | mdr | remote_access | backups | disk_encryption | warp
  uint64 host_id = 3;          // MUST be a host_id the host handed this plugin (no fabrication)
  string state = 4;            // protected | at_risk | not_installed | unknown
  string detail = 5;
  int64  collected_at = 6;     // unix
}
```
(WASM: the same shape as Extism host-function imports + a `describe`/`sync` export; the bytes-in/bytes-out
model maps cleanly to these messages.)

## 9. Phased plan

1. **Phase A (now):** in-process registry + `HostStatusProvider` + `host_integration_status` table + read
   path (already the MVP plan). Prove the coverage matrix end-to-end with BitLocker + one collector.
2. **Phase B:** freeze the **Tier-1 Plugin API v1 proto** + build the **adapter/version-firewall** and the
   **in-process** backing behind it (so first-party providers already run *through* the stable interface —
   this is what makes the boundary real and tested).
3. **Phase C:** add the **WASM (Extism/wazero) loader** + signing/verification + the least-privilege host
   functions + write-scoping — the first *distributable* compiled collector.
4. **Phase D:** **gRPC (go-plugin)** escape-hatch backing for native/CGO plugins.
5. **Upstream (anytime):** open the RFC/issue for the thin host-integration-status slice; independent of A–D.

## 10. Open items to verify before coding
- Extism manifest field names for `allowed_hosts`/`allowed_paths` (grant schema).
- Go-as-WASM-guest maturity (TinyGo limits) if we want partners authoring Go collectors; Rust/JS are the mature guest paths today.
- Benchmark the WASM host-call boundary for realistic batch upserts (thousands of rows/sync).
- Signing infra decision (cosign/sigstore vs Fleet-managed key), key mgmt + revocation + partner-signing workflow.

## Citations
- Go `plugin` limits: pkg.go.dev/plugin; golang/go#27751.
- hashicorp/go-plugin (handshake/protocol-versioning/checksum+TLS/crash-isolation): github.com/hashicorp/go-plugin.
- Extism (WASI opt-in, host-controlled HTTP, untrusted-code): extism.org/docs; wazero (pure-Go/no-CGO, 1.0 Mar 2023): wazero.io.
- osquery extension SDK (Thrift, ownership checks, privilege separation, deregistration): osquery.readthedocs.io.
- Grafana plugin signing (MANIFEST.txt, unsigned-not-loaded, trust tiers): grafana.com/docs.
- Repo precedent: `orbit/pkg/table/extension_darwin.go` (Fleet statically compiles the macadmins osquery-extension); `server/service/osquery_utils/queries.go:183`; `server/fleet/datastore.go:1234-1248`; `cmd/fleet/serve.go:725`.
