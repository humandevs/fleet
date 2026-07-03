# PREMIUM-ARCHITECTURE.md — building EE-equivalent modules as a provider, not core patches

> How we rebuild the premium features without lock-stepping Fleet's core files. Code-verified. Answers
> "can we do a minimal rearchitect / plugin architecture / separate-repo extensions?" Companion to
> [FORK-STRATEGY.md](./FORK-STRATEGY.md), [PLUGIN-API-RFC.md](./PLUGIN-API-RFC.md), [RISK-REGISTER.md #19](./RISK-REGISTER.md).

## TL;DR

- **Fleet's premium layer is already a one-point dependency-injection seam** — `ee/` is just one
  implementation plugged into it. We write **our** implementation as a drop-in provider in a **new
  `server/premium/` bundle** (Tier-3, rebase-safe), wired by swapping the **one composition call** at
  `serve.go:577` (+ import + 2 `cron.go` migration funcs).
- This **decouples the WIRING** (one seam, our clean-room code, drift-detecting contract tests) — it does
  **not** shrink the **semantic** cost: we still clean-room reimplement the premium bodies. That L–XL
  reimplementation is the irreducible fork cost (RISK-REGISTER #19); the bridge just guarantees it never
  scatters across core files.
- **Don't upstream the registration hook** — it saves ~1 line and Fleet is unlikely to take it. Carry the
  tiny swap as a Tier-5 patch.

## 1. The seam (verified)

Core enables premium via three MIT mechanisms, **never by editing its own service-file bodies**:
- **~100–150 stub methods** on the `fleet.Service` interface returning `fleet.ErrMissingLicense`
  (e.g. `teams.go:48 ListTeams`), **shadowed** by an embedding decorator.
- A **14-field `EnterpriseOverrides`** func-pointer struct (`server/fleet/service.go:21`), populated via
  `SetEnterpriseOverrides`.
- **One composition call**: `cmd/fleet/serve.go:577` `eeservice.NewService(svc, …)` — 19 positional args,
  inside `if license.IsPremium()`.

> ⚠️ **Nil-panic caveat:** ~30 `EnterpriseOverrides` call sites **deref directly (no nil guard)** —
> `apple_mdm.go`, `appconfig.go`, `mdm.go`, `scripts.go`, `orbit.go`, `osquery.go`,
> `windows_mdm_profiles.go`. They're safe in free tier only because their branch is unreachable without
> teams/premium. **Our `premium.NewService` MUST populate all 14 fields** or it panics at runtime.

## 2. The provider-bridge design

A new **`server/premium/`** bundle (all Tier-3 new files, per FORK-STRATEGY Tier 3):
- **`server/premium/service/service.go`** — the **one bridge** (in-process version-firewall):
  `type Service struct { fleet.Service; ds fleet.Datastore; <premium sub-services> }`. Embedding the core
  `fleet.Service` auto-satisfies the whole interface; the bridge shadows only the premium methods
  (clean-room, organized by domain: `teams.go`, `software_installers.go`, `mdm.go`, `vpp.go`, `scripts.go`,
  `setup_experience.go`, `conditional_access.go`, `certificate_authorities.go`, `scim.go`). **This is the
  ONLY file touching core's premium contract.**
- **`premium.NewService(core fleet.Service, deps premium.Deps) (fleet.Service, error)`** — accepts the same
  **19 deps** `serve.go` already constructs (all MIT/exported: `ds`, `logger`, `config`, `mailService`,
  `clock`, `depStorage`, `MDMAppleCommander`, `ssoSessionStore`, `profileMatcher`, `softwareInstallStore`,
  `bootstrapPackageStore`, `softwareTitleIconStore`, `distributedLock`, `KeyValueStore`, `scepConfigMgr`,
  `digiCertService`, `androidSvc`, EST/`hydrantService`). Constructs the premium sub-services, then
  `core.SetEnterpriseOverrides(fleet.EnterpriseOverrides{ …all 14 bound to itself… })`.
- **`server/premium/*` domain sub-packages** behind our own internal interfaces, so the bundle stays
  seam-agnostic except for the bridge.
- **Contract tests** (§4).

**Wiring change** (the only core touch): swap `serve.go:30` import + `serve.go:577` call
(`eeservice.NewService` → `premium.NewService`) and the two standalone `cron.go` funcs
(`UninstallSoftwareMigration`/`UpgradeCodeMigration` at `:2206/:2234`). Small, localized Tier-5 patches on
churny files — a rebase conflict here is *desirable* (loud).

## 3. Gating taxonomy — three kinds, zero core-body edits

| Kind | How core exposes it | How we handle it | Core edit |
|---|---|---|---|
| **Inline `license.IsPremium(ctx)`** (~186 sites) | branch inside a core method | the **always-premium license validator** flips them on (separate seam, not the bridge) | none |
| **`ErrMissingLicense` stub methods** (~100–150) | a method on `fleet.Service` | bridge **embeds + shadows** (method promotion) | none |
| **`EnterpriseOverrides` func-fields** (14) | nil func pointer | bridge **populates** via `SetEnterpriseOverrides` | none |

> **Graceful growth:** when upstream *adds* a new premium method, the bridge doesn't compile-break — the
> unshadowed method **falls through to the embedded core stub** (free-tier `ErrMissingLicense`). Implementing
> it becomes a *product decision*, not a forced lock-step.

## 4. Coupling surface (honest)

After the rearchitect the bridge couples to core at exactly **three surfaces, all in
`server/fleet/service.go`, none in `server/service/*.go` bodies**:
1. **The `fleet.Service` interface** — embedding makes growth nearly free (new methods fall through). We
   break only on a *signature change to a method we shadow* → a **loud compile error** (core recompiles too).
2. **The `EnterpriseOverrides` struct field-set — the sharpest residual coupling.** Adding a field is **not
   a compile error** (Go named-field literals don't require all fields), so a new field with an un-guarded
   call site **silently panics at runtime after a rebase.** → **A reflection contract test asserting every
   field is non-nil post-construction is mandatory**, converting the crash into a test failure.
3. **The 19-arg constructor** — self-consistent (we own both ends via the swap); shifts only if upstream
   changes the deps at `serve.go:577` (localized, compile-time).

**What is NOT decoupled:** the **semantic surface**. The bridge makes the *wiring* one seam; it does nothing
for the ~14 override bodies + ~100+ shadowed methods we clean-room reimplement and maintain **forever**
(RISK-REGISTER #19). Even with a field present and populated, upstream can change what a method must *do*
(escrow flow, VPP validation) and no test catches behavioral divergence from unreadable `ee/`. **Do not
misread this rearchitect as a cost-saver** — it hardens and clean-rooms the seam; it doesn't shrink the
reimplementation.

**Contract tests:** (1) reflection over `EnterpriseOverrides` → all 14 fields non-nil after
`premium.NewService`; (2) `var _ fleet.Service = (*premium.Service)(nil)`; (3) a golden-list parity test
(AST-derive the core `ErrMissingLicense` stubs vs the methods the bridge shadows, so a newly-added premium
stub surfaces in review); (4) `serve.go` deps still satisfy `premium.Deps`. Run `go test ./server/service/`
after any new datastore method.

## 5. Your `/extensions/` folder + separate-repo idea

Two levels, and the decider is Go's `internal/`-package rule:

- **In-tree `extensions/ourco/` (or `server/premium/`) — do this now.** Same Fleet module path, so it has
  **full access** (including `internal/` packages), and it's rebase-safe (new files). `ee/` becomes just one
  provider dir among ours. This is the pragmatic answer today.
- **Separate repo / Go module — the cleaner endgame.** A separate module (`github.com/ourco/…`) can import
  only Fleet's **exported, non-`internal/`** API. `ee/` gets more only because it lives under the same module
  path. **The good news:** the entire wiring contract — the 19 constructor args, `fleet.Service`,
  `EnterpriseOverrides`, `fleet.Datastore` — **is all exported**, so it crosses a module boundary fine. And
  **because *we* write the premium bodies clean-room, we control what they import** — if our implementations
  stick to Fleet's exported API + our own code, a **separate repo is feasible**. The only blocker is any spot
  where Fleet's exported surface is insufficient and we'd need an `internal/` helper — which either forces an
  **upstream "export this" PR** or keeps that one piece in-tree.
- **Phased:** in-tree `server/premium/` now → identify + upstream any `internal/` gaps in the contract →
  externalize to a separate repo once the exported surface is sufficient. At that point our premium code has
  **zero presence in the Fleet tree** — we just track Fleet as a versioned dependency. Compile-time
  composition = blank-import the module (`import _ "github.com/ourco/premium"`) so its `init()` registers,
  then `go build`. (This is **compile-time**, distinct from the runtime WASM/gRPC Plugin API for third-party
  *collectors* — that layer can't carry deep-datastore premium logic.)

## 6. Upstream: skip the registration hook

Replacing `serve.go:577` with a `RegisterPremiumProvider(factory)` hook is **technically trivial** (same
func-pointer idiom Fleet uses for `EnterpriseOverrides`). But: **merge odds LOW** (Fleet owns both
`serve.go` and `ee/`, so they get near-zero benefit; it reads as abstraction for an external party), and
**even if merged it saves ~1 line** — the seam is *already* a single call. **Recommendation: carry the
2-line swap as a Tier-5 patch; don't spend upstream energy here.** Reserve that energy for the genuinely
additive **`HostStatusProvider`** seam (PLUGINS.md §4.2), which fills a real Fleet gap.

## 7. The irreducible cost stands

| Component | Build | Effort |
|---|---|---|
| The bridge (embed + populate 14 + 19-arg constructor) — the **wiring shell** | Tier-3 clean-room | **S–M (cheap)** |
| `serve.go` + `cron.go` swap | Tier-5 patch | S |
| Contract tests | Build | S–M |
| **Premium method reimplementations** (Teams, software, MDM enforcement, VPP, SCIM, cond-access, CAs, …) | **Clean-room** | **L–XL (the real cost — unchanged)** |

**Minimize the L–XL by building only the premium features you'll actually sell**, keep them behind the one
seam, and **track upstream's EE CHANGELOG/CVEs** so you know when a clean-room re-derivation is needed
(clean-room bars cherry-picking upstream's premium security fixes).

## Verified receipts
- `EnterpriseOverrides` struct + `SetEnterpriseOverrides`: `server/fleet/service.go:21`; embedded `*EnterpriseOverrides` in core `Service` `server/service/service.go:56,114-116`.
- Nil-guarded sites: `appconfig.go:2474`, `osquery.go:1251`, `policies.go:92`, `queries.go:304/454/…`, `team_policies.go:117/…`. Un-guarded (panic) sites: `apple_mdm.go:392/887/1453/…`, `mdm.go:1536/…`, `scripts.go:437/…`, `orbit.go:1153/1621`, `osquery.go:2268/2334`, `windows_mdm_profiles.go:45`.
- Composition: `cmd/fleet/serve.go:500` (`if license.IsPremium()`), `:577` (`eeservice.NewService`, 19 args), `:612/:618` (`SetActivityService`/`SetACMEService`, fall through to core). `cmd/fleet/cron.go:14,2206,2234`.
- Route dispatch is polymorphic through `fleet.Service` (`MakeHandler`, `listTeamsEndpoint(…, svc fleet.Service)`) → embedding shadows with zero route edits.
