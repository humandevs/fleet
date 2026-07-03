# FORK-STRATEGY.md — keeping the fork rebasable against fast-moving upstream

> How we carry our changes so upstream Fleet (hundreds of commits between our rebases) stays mergeable.
> The Go-native realization of your "patch-package + modules" instinct. Code-verified. Companion to
> [RISK-REGISTER.md #19](./RISK-REGISTER.md) and [OSS.md](./OSS.md).

## TL;DR

- **`patch-package` is the wrong tool** — it's npm/yarn-only (applies committed `.patch` files to
  `node_modules` via a postinstall hook). It doesn't touch Go source or a whole-repo fork.
- The Go-native equivalent is a **precedence ladder** — always take the **highest tier that works**:
  **(1) upstream PR → (2) config/build-time not source → (3) new files in our superset dirs →
  (4) `go build -overlay` whole-file replacement → (5) managed git patch-stack.**
- **`go build -overlay` is real** and is exactly your "modules that overwrite built-in files" idea — but it
  has hard limits (below), so it's for **whole-file, low-churn, non-logic** swaps only.
- **The honest bottom line:** the ladder keeps rebrand, license-ungating, and new features rebasable — but
  the **irreducible fork cost is the clean-room premium reimplementations** (Teams, software, MDM, SCIM…)
  that fill Fleet's churniest core files, and *no tier makes those cheap*. Plan around that, not around the
  easy 90%.

## The precedence ladder

### Tier 1 — Upstream PR
Contribute it to `fleetdm/fleet`. Zero long-term maintenance. Candidates: the `HostStatusProvider` seam, a
license-agnostic cron group, the activities-webhook HMAC option, extending `mdm_config_assets` encryption
to integration secrets. Cost: review latency + rejection risk.

### Tier 2 — Config / build-time, NOT source
Behavior without editing tracked files → **rebase never conflicts** (no tracked change). Verified in-repo:
the binary name is `-X …/server/version.appName=${APP_NAME}` (`Makefile:64`) — **rebrandable via ldflags,
no source edit**. Also: env/config flags, asset swaps, `GOFLAGS`.

### Tier 3 — New files only, in our namespace
Files upstream will never create: `server/integrations/`, `server/licensing/`,
`server/endpointprotection/`, `human/`, and **new migration files**. Rebase-safe as long as filenames never
collide. Go `init()` auto-registration means new files wire themselves in without editing an upstream
registrar. **Maximize this tier.**

### Tier 4 — `go build -overlay` (the "modules" mechanism) ✅ confirmed real
`-overlay <file>` reads JSON `{"Replace": {"/abs/disk/path.go": "/abs/backing/override.go"}}` and builds as
if each disk path had the backing file's contents (empty backing = deleted). **Whole-file replacement at
compile time without modifying the on-disk file — `git status` stays clean, rebase never conflicts on the
overlaid file.** Our mechanism (all Tier-3 new files):
- `fork/overlay/manifest.json` (the map), `fork/overlay/<mirrored path>/<file>.go` (full copy of the
  upstream file with our targeted edits), `fork/overlay/gen.go` (emits absolute-path JSON for this checkout).
- Wire via a **new `make fork-build` target** (in `fork/Makefile.fork`) that re-invokes the exact upstream
  `go build` line (`Makefile:209/212/221`) plus `-overlay=…` — zero edit to the upstream Makefile.
- **Frontend equivalent:** webpack `resolve.alias` (already used, `webpack.config.js:148-153`) + mirror in
  `tsconfig` `paths` so the type-checker agrees.

> ⚠️ **`-overlay` hard limits (why it's not a silver bullet):** (1) **NOT honored by `go test`/`go run`** —
> overlaid logic is *untested by the standard suite*. (2) **Whole-file only** — no line-diff, so an overlaid
> copy **silently goes stale** when upstream edits that file. (3) Can't replace `GOMODCACHE` files.
> (4) The `fork-build` wrapper must **duplicate the upstream recipe** (tags/ldflags/CGO) → it silently
> **drifts** if upstream changes the recipe → add a CI check that the wrapper's flags still match.

### Tier 5 — Managed git patch-stack
For edits that must *modify* an existing file's lines (and where you *want* rebase to surface conflicts, not
hide drift): an ordered patch series on a pinned upstream tag — **quilt** (`patches/series`) or
`git rebase --onto <new-tag> <old-tag> fork-branch`. Exactly how Debian, Chromium, and AOSP carry patches.
Keep each patch small, single-purpose, reviewable.

### Tier 4 vs Tier 5 (the key tradeoff)
Overlay keeps the tree pristine and never conflicts **but hides drift and `go test` never runs it**.
Patch-stack physically edits the file, so **tests cover it AND rebase raises a conflict the moment upstream
moves the touched lines** — usually what you want for logic. **Rule: Tier-4 for whole-file swaps of
low-churn/non-logic files (license-validator selection, brand assets, generated files); Tier-5 for
line-level logic edits to churny files. Lean hardest on Tiers 1–3 so 4–5 stay tiny.**

## Applied to our actual changes

| Change | Tier | Verified reality |
|---|---|---|
| **Binary name** | 2 | ldflags `version.appName` (`Makefile:64`) — clean build-time. |
| **License = always-premium** | 2/3 | **One validator flips all ~186 `IsPremium` gates** (they read one `LicenseInfo.Tier` set once by `initLicense→LoadLicense`). Replace `ee/server/licensing` with our own → zero gate-body edits. **But the *wiring* lives in `serve.go:28,1284` — Fleet's churniest file** → a small Tier-5 patch in the churn zone, not truly edit-free. |
| **Rebrand strings** | ⚠️ **not build-time** | **1,535 "Fleet" lines across 387 frontend files + 130 `fleetdm.com` Go lines, decentralized, no central brand table.** Only the binary name + assets are clean build-time. Strings need either a **string-table refactor (upstreamable Tier-1)** or per-string Tier-5 patches. Don't assume rebrand is free. |
| **`X-Fleet-License` header** | protocol | It's **one constant** (`app.go:1802`) but a **wire-protocol header** (server sets it, fleetctl/orbit read it). **Rename the display, keep the wire value** — changing the value breaks server↔agent compat unless all rebuilt in lockstep (controllable for instance-per-MSP matched builds). |
| **Sever `ee/` imports** (serve.go×9, cron.go×2) | 5 | Line-level edits to churny files → Tier-5 patches. **But see the dominant cost below.** |
| **New migrations** | 3 | New files, but Fleet's `compareMigrations` boot guard (`mysql.go:610`) rejects a DB with migrations the binary doesn't know → use a **reserved high-number band** for fork migrations (sorts after upstream, collision-free, always "known" to the fork binary). |
| **Integrations, plugin host, endpoint-protection, coverage matrix** | 3 | New packages in our dirs — the bulk of our net-new code, rebase-safe. |

## ⚠️ The dominant, irreducible cost the ladder can't fix

The verification caught the load-bearing understatement: **"sever `ee/` and flip `IsPremium=true`" is in
direct tension.** The premium *feature implementations* live in `ee/`, wired via `EnterpriseOverrides`
(`server/fleet/service.go:21`) as func-fields. Stub out the `ee/` call sites and those fields are **nil** —
so flipping the tier to premium exposes gates/UI for features **with no backend**. A commercial UEM that
**sells** premium features must **clean-room reimplement** them (Teams, software installers, MDM
enforcement, SCIM, conditional access, cert authorities) — **effort L/XL, concentrated in Fleet's churniest
files**, maintained *forever* in parallel, with clean-room barring cherry-picking upstream's own security
fixes (RISK-REGISTER #19).

**No tier of this ladder makes that cheap** — overlay can't (it's logic edits to churny files); patch-stack
can carry it but with real ongoing conflict cost. So:
- The ladder governs the *easy 90%* (rebrand, license, new features, integrations) — keep those in Tiers 1–3.
- The premium reimplementation is the *hard 10%* that dominates fork maintenance. **Minimize how much of it
  you actually build** (only the premium features you'll sell), keep it in **new packages** (Tier-3) wired
  through `EnterpriseOverrides` rather than editing core, and **track upstream's EE CHANGELOG/CVEs** so you
  know when a clean-room re-derivation is needed. Re-confirm the build-vs-license-EE-vs-stay-upstream
  trade-off (RISK-REGISTER #19) is still worth it for each premium feature before rebuilding it.

## Components / effort
| Component | Build/reuse | Effort |
|---|---|---|
| `go build -overlay` modules (manifest + overrides + `make fork-build` + CI recipe-drift check) | Build (Tier-3/4) | M |
| Frontend overlay (webpack alias + tsconfig paths) | Reuse seam | S |
| Build-time rebrand (binary + assets) | Reuse (Tier-2) | S |
| Clean-room license validator (Tier-3 new pkg) + serve.go wiring patch (Tier-5) | Build | M |
| Reserved-band migration numbering convention | Convention | S |
| `ee/`-severance patch series (Tier-5) | Build | M |
| **Premium feature reimplementations (Teams/software/MDM/SCIM…)** | **Build clean-room** | **L–XL (the real cost)** |

## Citations
- `go build -overlay`: `go help build` / go-build docs (JSON `Replace` map; not honored by `go test`/`go run`; no GOMODCACHE).
- Debian quilt / Chromium / AOSP patch-carry playbooks.
- Repo: `Makefile:64,95,209,212,221`; `server/fleet/app.go:1764,1802`; `server/contexts/license/license.go:41`; `cmd/fleet/serve.go:28,1284`; `server/fleet/service.go:21`; `server/datastore/mysql/mysql.go:610,658-676`; `webpack.config.js:146,148-153`.
