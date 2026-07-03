# OSS.md — Forking FleetDM into a self-hosted UEM platform

> **Status:** Discovery / planning. This document is an *engineering* assessment of what
> Fleet's open-source (MIT) edition can and cannot do, the licensing constraints on forking
> and selling it, and a rough design for the integrations we want to build. It is **not legal
> advice** — the licensing section flags exactly which two items (clean-room rebuilds and
> trademark) need sign-off from IP counsel before commercial release.
>
> Produced from a multi-agent audit of the repository at `main` (commit `ef0aed3b3f`, June 2026).
> Every claim below is grounded in a real file path so it can be re-verified.

---

## Table of contents

1. [Executive summary](#1-executive-summary)
2. [The licensing picture (can we fork and sell it?)](#2-the-licensing-picture)
3. [How the OSS/EE split actually works (the one diagram that matters)](#3-how-the-ossee-split-actually-works)
4. [Feature inventory: free vs. gated vs. absent](#4-feature-inventory)
5. [What we inherit for free (the foundation)](#5-what-we-inherit-for-free)
6. [What we must rebuild (the proprietary gaps)](#6-what-we-must-rebuild)
7. [Hosted-cloud dependencies we must replace](#7-hosted-cloud-dependencies-we-must-replace)
8. [Integration designs](#8-integration-designs)
   - 8.1 [Software & patch: winget / Chocolatey / Ninite Pro / Action1](#81-software--patch-management)
   - 8.2 [AV/EDR dashboards: Bitdefender GravityZone + Huntress](#82-avedr-dashboards-bitdefender-gravityzone--huntress)
   - 8.3 [Android UEM rebuild](#83-android-uem-rebuild)
   - 8.4 [Mosyle MDM for iOS/macOS](#84-mosyle-mdm-for-iosmacos)
   - 8.5 [IPaaS onboarding/offboarding (future, external)](#85-ipaas-onboardingoffboarding-future)
9. ["Plugins folder" question — where integration code should live](#9-where-integration-code-should-live)
10. [Recommended phasing / roadmap](#10-recommended-phasing--roadmap)
11. [Risks & open questions](#11-risks--open-questions)
12. [Appendix A: key file map](#appendix-a-key-file-map)
13. [Appendix B: how this analysis was produced](#appendix-b-how-this-analysis-was-produced)

---

## 1. Executive summary

**A sellable fork is legally feasible, and the technical seam between free and paid is unusually
clean.** Three findings drive everything else:

1. **Licensing is favorable.** Everything outside `ee/` and `docs/` is **MIT Expat** — free to
   use, modify, distribute, *and sell*. The entire React/TypeScript frontend is MIT *even where it
   renders premium features*. There are **no GPL/AGPL/LGPL dependencies**. The only hard
   constraints are (a) you may not copy/sell any server- or agent-side Go under `ee/`, and (b)
   **trademark** — you must rebrand off "Fleet." (`LICENSE`, `ee/LICENSE`)

2. **The free core is already a real UEM.** Out of the box and license-free you inherit: osquery
   fleet management, the full multi-source CVE detection engine, the scripts engine, software
   inventory, the **Apple MDM protocol stack** (enroll/commands/APNs/SCEP/profiles/DDM), **full
   Windows MDM auto-enrollment**, and — a major and easily-missed win — a **complete Android
   Enterprise (AMAPI) implementation with zero license gates** (`server/mdm/android/`). SSO/SAML is
   also free.

3. **What's paid is mostly "business logic on top," and the gate is a single seam.** Premium
   features are gated at the *service method* layer via one runtime check, `license.IsPremium(ctx)`
   (`server/contexts/license/license.go`). Some premium behavior is just a gated branch inside MIT
   core (flip it on by changing what the license check returns). The rest is implemented only in
   `ee/server/service/` (43 files) and must be **rebuilt clean-room** — chiefly multi-tenancy
   (Teams/Fleets), advanced MDM (FileVault/BitLocker escrow, OS-update enforcement, zero-touch
   setup, VPP), software deployment, SCIM, and conditional access.

**Bottom line:** the cheapest viable product is "delete `ee/`, replace the license validator,
rebrand, ship the free core as a single-tier UEM," then add back paid-tier capabilities clean-room
as the business justifies. Our headline integrations (winget/Choco/Ninite, Action1, Bitdefender,
Huntress, Android, Mosyle) are **all net-new MIT code that hooks into existing extension points** —
none of them require touching `ee/`.

---

## 2. The licensing picture

### 2.1 What each part of the repo is licensed under

| Path | License | Can we sell it? |
|------|---------|-----------------|
| Everything outside `ee/` and `docs/` (server, datastore, `orbit/` agent, vendored MDM libs) | **MIT Expat** | ✅ Yes — use/modify/distribute/sell |
| **All compiled/served client-side JavaScript** (the whole `frontend/`, even premium-gated screens) | **MIT Expat** (explicit carve-out, `LICENSE:7`) | ✅ Yes |
| `ee/` server-side & agent-side Go (`ee/server/`, `ee/orbit/`, `ee/pkg/`) | **Fleet EE License** (proprietary) | ❌ No — cannot copy, distribute, sublicense, or sell |
| `ee/fleetd-chrome` (TypeScript → browser JS) | MIT *via the client-side-JS carve-out* | ⚠️ Yes, but **carve-out-dependent** — relocate out of `ee/` and get counsel sign-off |
| `docs/` | **CC BY-SA 4.0** | ⚠️ Only with attribution **and** ShareAlike — easier to drop and write our own |
| Vendored MDM libs (`server/mdm/nanomdm`, `nanodep`, `scep`), `server/goose/`, `orbit/` | MIT (own LICENSE files) | ✅ Yes — keep their attribution |

**Copyleft check (clean):** the module graph has **no GPL/AGPL/LGPL** dependencies. A handful of
HashiCorp libs are **MPL-2.0** (`go-multierror`, `go-version`, `golang-lru`, `errwrap`, `go-hclog`)
— file-level copyleft only, which does **not** touch our proprietary code in a sold binary unless we
modify those specific library files. `golang-jwt/jwt/v4` (used by the license validator) is itself
MIT and reusable.

### 2.2 The verdict: yes, with discipline

You **can** legally fork, rebrand, and sell the MIT core. The two — and only two — areas with real
legal exposure are:

- **Clean-room rebuilds.** Any premium feature you reimplement must be written from the *public*
  `fleet.Service` interface (MIT) and observed behavior, **never** by reading and translating
  `ee/` source. Keep a written clean-room log (who read `ee/` vs. who implemented) as a defense.
- **Trademark.** MIT grants copyright rights, **not** trademark. "Fleet," its wordmarks, logos, and
  "Fleet dots" must be fully replaced. This is the single most overlooked item — people assume MIT
  covers the name; it does not. (`handbook/company/brand.md`)

### 2.3 The fork-and-sell checklist (ordered)

1. **Delete the `ee/` Go/agent tree** (`ee/server/`, `ee/orbit/`, `ee/pkg/`, `ee/tools/`, `ee/cis/`,
   `ee/maintained-apps/`, `ee/fleetctl/`, `ee/fleet-agent-downloader/`, `ee/vulnerability-dashboard/`)
   and `ee/LICENSE`. Relocate `ee/fleetd-chrome` out of `ee/` if you want it (it's MIT via carve-out).
2. **Delete `docs/`** (CC BY-SA) unless you'll comply with attribution + ShareAlike. Write our own.
3. **Sever every `ee/` import from MIT entrypoints** so the binary compiles without `ee/`. The audit
   found the OSS binary does **not** build without `ee/` today. The complete list (corrected during
   verification — the first pass undercounted):
   - `cmd/fleet/serve.go` imports **9** ee packages (lines 28–36): `ee/server/licensing`,
     **`ee/server/scim`** (line 29, easy to miss — used at `serve.go:884` `scim.RegisterSCIM`),
     `ee/server/service`, `.../service/condaccess`, `.../digicert`, `.../est`, `.../hostidentity`,
     `.../hostidentity/httpsig`, `.../scep`.
   - `cmd/fleet/cron.go` imports **2**: `ee/server/service` (line 14; used via standalone migration
     funcs `UninstallSoftwareMigration`@2206, `UpgradeCodeMigration`@2234 — *not* the service
     wrapper) and **`ee/server/webhooks`** (line 15, the enriched vuln payload mapper).
   - `cmd/fleetctl/fleetctl/preview.go` imports `ee/server/licensing` (line 28).
4. **Build a clean-room license validator** in a non-`ee/` package (e.g. `server/licensing/`).
   Generate *our own* ECDSA/Ed25519 keypair, embed *our* pubkey, reimplement `LoadLicense` to return
   `*fleet.LicenseInfo`. Reuse `golang-jwt` (MIT). Do **not** copy `ee/server/licensing/pubkey.pem`
   or its `validate()`. Repoint `cmd/fleet/serve.go:1284 initLicense()` and **delete the embedded
   Fleet-signed dev-license JWT** (`serve.go:1277-1282`). **Decided approach (single-tier, re-tierable):**
   make the validator return `TierPremium` **when a config flag is set** (default on for our internal
   build) — this instantly unlocks every `license.IsPremium(ctx)` branch and all 172 frontend
   `isPremiumTier` paths with zero `ee/` code, **while leaving every gate in place** so flipping the flag
   off (plus wiring real license claims) re-enables tiering later. Do **not** delete the gates or inline
   premium logic into core.
5. **Rebuild premium features clean-room** (only the ones we want to sell) — see §6.
6. **Rebrand completely** — product name, logos, the `X-Fleet-License` header
   (`server/fleet/app.go:1802`), and the pervasive `fleetdm.com` URLs hardcoded in user-facing error
   strings (`server/fleet/errors.go:23-27` — `MDMNotConfiguredMessage`, `AppleMDMNotConfiguredMessage`,
   etc.; the scrub is larger than a few constants), plus `orbit/pkg/packaging` installer metadata.
7. **Ship correct LICENSE + NOTICE** — a clean MIT license for our new code that *retains* the
   required upstream MIT copyright notices: Fleet Device Management Inc + Kolide (root), Jesse
   Peterson (nanomdm/nanodep), Victor Vrantchan (scep), Vojtech Vitek / Liam Staskawicz (goose).
   Generate a `THIRD-PARTY-NOTICES` from `go.mod`.

> ⚖️ **Engage IP counsel on exactly two things:** the clean-room process for the `ee/` rebuilds, and
> trademark clearance for the new product name. The MIT/copyright analysis itself is settled and
> favorable.

---

## 3. How the OSS/EE split actually works

This is the most important thing to internalize, because it determines how much work each feature is.

```
            HTTP request
                │
   server/service/handler.go  ── routes are ALL registered in MIT core, unconditionally
                │                 (a route existing tells you NOTHING about whether it works)
                ▼
        svc.SomeMethod(ctx, …)        fleet.Service interface  (MIT)
                │
        ┌───────┴────────────────────────────────┐
        │                                         │
   CORE STUB (MIT)                         EE OVERRIDE (proprietary)
   server/service/*.go                     ee/server/service/*.go
   returns fleet.ErrMissingLicense  ◄──── embeds fleet.Service and SHADOWS the stub
   (HTTP 402)                              wired in cmd/fleet/serve.go:577 eeservice.NewService
```

Two distinct gating styles — and they cost very different amounts to unlock:

- **Style A — gated branch inside MIT core.** The real logic lives in core; a `license.IsPremium(ctx)`
  check just refuses it on the free tier. Example: audit-log streaming
  (`cmd/fleet/logging.go:75`), MFA enable (`server/service/users.go:715`), CVSS/EPSS/CISA vuln scores
  (`server/vulnerabilities/nvd/sync.go:316`), label-scoped queries/policies. **Cost to unlock: S** —
  flip the license check (or have our validator report premium). The code is already ours (MIT).

- **Style B — implementation lives only in `ee/`.** The core method is a *stub* that returns
  `ErrMissingLicense`; the real body is in `ee/server/service/`. Example: Teams, software installers,
  VPP, FileVault escrow, setup experience, SCIM, conditional access, host lock/unlock/wipe. **Cost to
  unlock: L–XL** — must be rebuilt clean-room; the route and the datastore exist, but the business
  logic does not.

Two clean seams make Style B tractable:

- **`EnterpriseOverrides` struct** (`server/fleet/service.go:21-44`) — a set of function pointers the
  core service calls for premium behavior that can't be added by embedding. Our fork supplies its own
  implementations and calls `SetEnterpriseOverrides` from its own constructor.
- **Service embedding/decorator** (`ee/server/service/service.go:18`) — `type Service struct {
  fleet.Service … }`. Defining a method on our wrapper shadows the core stub. This is exactly how Fleet
  layers premium today; we copy the *pattern*, not the code.

> **Crucial corollary:** the **database schema and datastore methods for premium features already
> exist in MIT core** (`server/datastore/mysql/teams.go`, `software_installers.go`, `vpp.go`,
> `scim_*`, `mdm_config_assets`, `host_disk_encryption_keys`). We rebuild *service-layer logic only*,
> not storage. That roughly halves the rebuild effort. It also means a naive "just flip
> `IsPremium=true`" exposes half-built paths whose service logic is absent → `ErrMissingLicense` or
> nil-pointer panics until each method is actually reimplemented.

---

## 4. Feature inventory

Legend: **🟢 Free** (MIT core, inherit as-is) · **🟡 Partial** (core branch gated by a license check —
flip to unlock, ~S) · **🔴 Rebuild** (implementation only in `ee/`, clean-room rebuild) · **⚪ Absent**
(greenfield).

### Device management / MDM

| Capability | Tier | Evidence / notes |
|---|---|---|
| Apple MDM protocol (enroll, checkin, command queue, APNs push) | 🟢 | `server/mdm/nanomdm/` (vendored MIT), `server/mdm/apple/commander.go` |
| Self-hosted SCEP CA (device identity certs) | 🟢 | `server/mdm/scep/depot/signer.go` — runs locally, no cloud |
| Apple config profiles (`.mobileconfig`) + DDM declarations | 🟢 | `server/service/apple_mdm.go`; premium only for *premium DDM variables* |
| **Windows MDM auto-enrollment (WSTEP/SyncML), CSPs, enrollment CA** | 🟢 | `server/service/microsoft_mdm.go`, `server/mdm/microsoft/wstep.go` — fully MIT, zero cloud |
| **Android Enterprise / AMAPI (enroll, policies, commands, BYOD, lock/wipe)** | 🟢 | `server/mdm/android/` — **zero license gates** (verified). Big free win. (team-scoping rides premium) |
| Disk-encryption key *ingestion* (FileVault & BitLocker) | 🟢 | `server/service/orbit.go postOrbitDiskEncryptionKeyEndpoint` |
| Apple ADE/DEP client (direct to `mdmenrollment.apple.com`) | 🟡 | client is MIT; setup-assistant CRUD + DEP sync are `ee/server/service/mdm.go` |
| Apple VPP client (direct to `api.ent.apple.com`) | 🟡 | client MIT; token/app mgmt in `ee/server/service/vpp.go` |
| FileVault **enforce + key escrow** | 🔴 | `ee/server/service/mdm.go` (EnterpriseOverride) |
| Windows **BitLocker enforce + escrow** | 🔴 | `ee/server/service/teams.go updateTeamMDMDiskEncryption` |
| OS-update enforcement (macOS/Win/iOS, Nudge/DDM) | 🔴 | EnterpriseOverrides `MDM*OSUpdates` |
| Zero-touch setup assistant / Setup Experience / bootstrap pkg / EULA | 🔴 | `ee/server/service/setup_experience.go` (531 lines) |
| ABM token management | 🔴 | `ee/server/service/mdm.go` (asset store is MIT) |
| MDM SSO / end-user IdP auth at enrollment | 🔴 | `ee/server/service/mdm.go` (SAML engine itself is MIT) |
| **Host lock / unlock** (all platforms) | 🔴 | stubs `server/service/scripts.go:1234/1263`; impl `ee/server/service/hosts.go` |
| **Host wipe** (macOS/Win/Linux/iOS) | 🟡/🔴 | Android COBO wipe is **free** (`scripts.go:1301`); other platforms premium |
| MDM commands (raw enqueue + results) | 🟢 | `server/service/mdm.go RunMDMCommand` (a few command types premium) |

### Software & patch

| Capability | Tier | Evidence / notes |
|---|---|---|
| Software inventory (osquery) | 🟢 | `server/service/osquery_utils/queries.go` |
| Software data model + datastore | 🟢 | `server/datastore/mysql/software_installers.go` |
| Scripts engine (saved scripts + run via orbit) | 🟢 | `server/service/scripts.go`; only *team-scoped* runs gated |
| Orbit install/script runner (msi/exe/pkg/deb/rpm + URL download) | 🟢 | `orbit/pkg/installer/installer.go`, `orbit/pkg/scripts/` |
| Software installer **orchestration** (upload/install/uninstall/self-service) | 🔴 | `ee/server/service/software_installers.go` (3912 lines — largest surface) |
| Fleet-maintained-apps catalog + ingesters | 🔴 | `ee/maintained-apps/` (EE-licensed *data* — cannot ship; rebuild from public winget/choco) |
| FMA runtime sync/download | 🟢 | `server/mdm/maintainedapps/sync.go` (repoint off `maintained-apps.fleetdm.com`) |
| VPP / Apple App Store apps | 🔴 | `ee/server/service/vpp.go` |
| In-house iOS/iPadOS apps | 🔴 | `ee/server/service/in_house_apps.go` |
| Native OS-update settings model | 🟢 | `server/fleet/app.go` (apply-to-team orchestration is 🔴) |
| Patch policy (auto-install newer version) | 🟡 | `pkg/patch_policy/` MIT; orchestration rides EE installer path |
| Microsoft Store / winget picker | ⚪ | only Apple VPP + Android tabs exist today |
| Action1 / external patch API | ⚪ | no references anywhere — greenfield |

### Monitoring / vulnerabilities / osquery

| Capability | Tier | Evidence / notes |
|---|---|---|
| Host enrollment + osquery TLS API | 🟢 | `server/service/osquery.go` |
| Live queries (Redis-backed) | 🟢 | `server/service/live_queries.go` |
| Scheduled queries / Reports | 🟢 | `server/service/scheduled_queries.go` |
| Host vitals (OS, hardware, disk-enc status, MDM status, users, network, battery) | 🟢 | `server/service/osquery_utils/queries.go` — the AV/EDR/UEM dashboard foundation |
| Log shipping (filesystem, webhook, Firehose, Kinesis, Lambda, PubSub, Kafka, NATS) | 🟢 | `server/logging/` |
| CVE detection (NVD/CPE, MSRC, OVAL, OSV, goval, MS Office) | 🟢 | `server/vulnerabilities/` — entire multi-source engine |
| CVSS / EPSS / CISA-KEV scores + CVE descriptions | 🟡 | `server/vulnerabilities/nvd/sync.go:316` — one license check; feeds + parsers already MIT |
| Policy queries + pass/fail | 🟢 | `server/service/global_policies.go` |
| Policy automation: failing-policy webhook + Jira/Zendesk | 🟢 | **free** — major remediation win |
| Policy automation: install-software / run-script / calendar on fail | 🟡/🔴 | fields are core; *execution* lives in `ee/` |
| Vuln host-count aggregation + scale primitives | 🟢 | `server/datastore/mysql/vulnerabilities.go` |

### Platform / access / integrations

| Capability | Tier | Evidence / notes |
|---|---|---|
| Versioned REST API + all route registrations | 🟢 | `server/service/handler.go` |
| **SSO / SAML** | 🟢 | `server/sso/` — fully MIT |
| API-only users + non-expiring bearer tokens + per-endpoint RBAC | 🟢 | `server/service/users.go`, `sessions.go:912`, `server/api_endpoints/` |
| Activities webhook (real-time) + pollable activity feed | 🟢 | `server/activity/internal/service/new_activity.go` |
| Users CRUD / invites | 🟢 | `server/service/users.go` |
| Labels incl. dynamic query-based smart labels | 🟢 | `server/service/labels.go` — interim grouping primitive before Teams |
| External integration config + connection-test pattern | 🟢 | `server/fleet/integrations.go`, `server/service/externalsvc/` |
| Cron/schedule framework + registration seam | 🟢 | `cmd/fleet/cron_registration.go`, `server/service/schedule/` |
| Per-host external data source (MacAdmins/Munki precedent) | 🟢 | `server/fleet/hosts.go MacadminsData` — the template for AV/EDR cards |
| Frontend (entire UI incl. premium screens, dashboard/host-detail cards, tier gating) | 🟢 | all of `frontend/` is MIT |
| **Teams / Fleets** (multi-tenant grouping) | 🔴 | `ee/server/service/teams.go` (2455 lines) — backbone of most premium scoping |
| GitOps / fleetctl (CLI is MIT; some objects gated) | 🟡 | `cmd/fleetctl/` MIT, but team/software specs hit EE endpoints |
| SCIM / IdP user+group sync | 🔴 | `ee/server/scim/` |
| Conditional access (Entra/Okta) | 🔴 | `ee/server/service/condaccess/` |
| Custom roles / RBAC (Observer+, Technician, GitOps, per-team, scoped API users) | 🔴 | gated `server/service/users.go:149`; authz engine itself MIT |
| MFA (console login) | 🟡 | machinery is MIT in `server/service/sessions.go`; only the enable-toggle is gated (`users.go:715`) |
| Audit-log streaming to SIEM | 🟡 | one gate `cmd/fleet/logging.go:75` |
| Jira/Zendesk ticketing, Google Calendar maintenance windows | 🔴 | `ee/server/service/` (an IPaaS replaces these anyway) |
| General outbound webhook / event-subscription system (multi-dest, HMAC, filtering) | ⚪ | only 4 fixed webhooks exist — greenfield if needed |

---

## 5. What we inherit for free (the foundation)

If we shipped the MIT core *today* (with the license validator stubbed to premium), as a single-tier
product we'd already have a credible UEM:

- **Cross-platform agent telemetry** via osquery — inventory, vitals, live + scheduled queries, log
  shipping to any SIEM.
- **Vulnerability management** — full CVE detection across NVD/MSRC/OVAL/OSV/Office; CVSS/EPSS/CISA
  scores are one `if`-removal away (§ Style A).
- **MDM you don't have to write:** the Apple MDM server, a self-hosted SCEP CA, Apple/Windows config
  profiles + DDM, **Windows automatic enrollment**, and a **complete Android Enterprise stack**.
- **Compliance & remediation basics** — policies, failing-policy webhooks, Jira/Zendesk tickets (the
  *notification* path is free), dynamic smart labels.
- **The entire web UI** — every screen, including premium-gated ones, plus the dashboard-card and
  host-detail-card frameworks we'll reuse for the AV/EDR dashboards.
- **A machine-friendly API** — non-expiring scoped tokens + a real-time activity webhook, ready for
  the future IPaaS.
- **SSO/SAML** for operator login.

That's the "Phase 1" product. Everything in §6 is incremental on top of it.

---

## 6. What we must rebuild (the proprietary gaps)

Prioritized by leverage. Effort: S/M/L/XL.

| # | Rebuild | Effort | Why / where |
|---|---|---|---|
| 1 | **Multi-tenancy (Teams/Fleets) service layer** | XL | `ee/server/service/teams.go`. The load-bearing dependency for team-scoped MDM, software, policies, enroll secrets, RBAC. **Datastore already exists** (`server/datastore/mysql/teams.go`). Spec this first and carefully — piecemeal rebuilds create inconsistent authz. |
| 2 | **Software deployment pipeline** | XL | `ee/server/service/software_installers.go` (3912 lines) + `vpp.go`. Upload/store/install/uninstall/self-service. Endpoints, datastore, orbit runner, payload types all MIT — it's a port against existing interfaces, not greenfield. Foundation for winget/Choco/Ninite (§8.1). |
| 3 | **Advanced MDM enforcement** | XL | ~40 EnterpriseOverride stubs: FileVault/BitLocker escrow, OS updates, DEP setup assistant, bootstrap/EULA, MDM SSO, ABM. Underlying primitives (commander, godep, asset store, SCEP CA) are MIT. |
| 4 | **Host lock / unlock / full-platform wipe** | L | `server/service/scripts.go:1234/1263/1301` → `ee/server/service/hosts.go`. Critical offboarding actions. Android wipe already free. |
| 5 | **Custom roles / RBAC** | M | Un-gate `server/service/users.go:149`; extend `server/authz/policy.rego`. ⚠️ security-review — partial un-gating risks privilege escalation. |
| 6 | **SCIM 2.0 server** | L | `ee/server/scim/`. Tables (`scim_users`/`scim_groups`) are MIT; rebuild the endpoint handlers. |
| 7 | **Conditional access (Entra/Okta)** | L | `ee/server/service/condaccess/`. Reuse the MIT proxy client `server/service/conditional_access_microsoft_proxy/`. |
| 8 | **Certificate authority integrations (NDES/SCEP/DigiCert)** | L | `ee/server/service/certificate_authorities.go` (1583 lines). For Wi-Fi/VPN cert deployment. |
| 9 | **Vuln scoring unlock + enriched webhook mapper** | S–M | Remove gate at `nvd/sync.go:316`; set `IsEE/useCVSScores` true in core; rebuild `ee/server/webhooks/mapper.go` (small). |
| 10 | **Audit-log streaming** | S | Remove the one check at `cmd/fleet/logging.go:75`. |
| 11 | **MFA enable** | S | Remove/flip `server/service/users.go:715`; machinery already MIT. |
| 12 | **Fleet-maintained-apps catalog** | L | Rebuild from public winget/Choco/Homebrew metadata; self-host the manifest CDN, repoint `server/mdm/maintainedapps/sync.go resolveBaseURLs()`. Do **not** ship `ee/maintained-apps/` data. |

---

## 7. Hosted-cloud dependencies we must replace

These are services on `fleetdm.com` that the code calls at runtime. A self-hosted product **must**
replace or remove each, or it silently depends on (and leaks telemetry to) Fleet's infrastructure.

| Dependency | URL / location | Replacement |
|---|---|---|
| **APNs vendor-CSR signing** | `https://fleetdm.com/api/v1/deliver-apple-csr` (`server/mdm/apple/cert.go`) | The one *hard* dependency — Apple requires the APNs cert be signed by a registered MDM vendor. **Lowest-effort fix:** drop the auto-CSR flow and use the existing upload endpoint `POST /mdm/apple/apns_certificate` (admin brings their own signed cert). Later: become a registered Apple MDM vendor. |
| **Android AMAPI proxy** | `https://fleetdm.com/api/android/` (`proxy_client.go`) | A **direct-to-Google client already exists** (`google_client.go`) but is dev-gated behind `FLEET_DEV_ANDROID_GOOGLE_CLIENT`. Promote it to default + first-class config (GCP service-account JSON + project). See §8.3. |
| **fleetd agent auto-update (TUF)** | `https://updates.fleetdm.com` (`orbit/pkg/update/update.go DefaultURL`) | Stand up our own TUF repo; rebrand installers (`orbit/pkg/packaging`). |
| **VPP token auth/metadata** | `https://fleetdm.com/api/vpp/v1/...` | Only relevant if we do Apple App Store deployment; the VPP token itself is admin-downloaded from ABM. |
| **CVE/CPE feeds** | Fleet's GitHub releases (`nvd/cve.go GetGitHubCVEAssetPath`) + configurable URLs | Self-host artifacts using the MIT `GenerateCVEFeeds` generator; point `CVEFeedPrefixURL`/`CPEDatabaseURL`/`CPETranslationsURL` at our infra. Review EPSS (`epss.cyentia.com`) and CISA feed terms for commercial redistribution. |
| **FMA manifest CDN** | `maintained-apps.fleetdm.com` (`maintainedapps/sync.go`) | Self-host our own catalog (§6 #12). |
| Usage analytics, "AI osquery SQL interpretation", fleetd-chrome `.crx` | various `fleetdm.com/...` | Disable or repoint. |

---

## 8. Integration designs

> **Operator setup guides** (credentials, tokens, install commands) for each vendor live in
> [`setup/`](./setup/): [Mosyle](./setup/mosyle.md) · [Action1](./setup/action1.md) ·
> [Bitdefender GravityZone](./setup/bitdefender-gravityzone.md) · [Huntress](./setup/huntress.md).

**The good news, restated:** every target integration is **net-new MIT code** that hooks into an
*existing* extension point. None require `ee/`. The patterns to reuse:

- **External APIs** → `server/service/externalsvc/` (mirror `jira.go`/`zendesk.go`, use
  `fleethttp.NewClient`); credentials in `server/fleet/integrations.go` (`AppConfig.Integrations`)
  with the mask-on-read / connection-test pattern.
- **Periodic sync** → register a schedule in `cmd/fleet/cron_registration.go` (a **license-agnostic**
  group — *not* `registerPremiumCrons`, which checks Fleet's license).
- **Per-host third-party status** → the **MacAdmins/Munki triad**: a side table keyed by `host_id`, an
  upsert datastore method, a per-host endpoint (`GET /hosts/{id}/...`) and an aggregated endpoint, a
  host-detail card + a dashboard card.
- **Software execution** → store an install/uninstall *script* on a `software_installer` row; the
  orbit agent runs it. No agent code changes.
- **New top-level page** → 3-file registration: `frontend/router/paths.ts` +
  `frontend/router/index.tsx` + `frontend/components/top_nav/SiteTopNav/navItems.ts`.

### 8.1 Software & patch management

**winget / Chocolatey / Ninite Pro are "package source + executor" integrations on top of the
existing scripts+installer pipeline.** They share one design:

- **Prereq:** rebuild the software-installer orchestration service (§6 #2) — or, minimally, the
  install path that queues a script + downloads a URL.
- **Executor (no agent changes):** an app installed this way is a `software_installer` row whose
  install/uninstall scripts are package-manager commands. `orbit/pkg/scripts/exec_windows.go` already
  runs PowerShell as SYSTEM.
  - **winget:** `winget install --id <id> --silent --accept-package-agreements --accept-source-agreements` / `winget uninstall --id <id>`
  - **Chocolatey:** `choco install <pkg> -y` / `choco uninstall <pkg> -y` (+ a one-time bootstrap snippet to ensure choco is present)
  - **Ninite Pro:** model each app as a **URL-based custom installer** — store the credentialed Ninite Pro download URL (`orbit DownloadSoftwareInstallerFromURL` already exists), run the `.exe` with silent flags. No catalog to ingest; lightest of the three.
- **Catalog/source:** for winget/Choco, generate our app catalog from the **public** `winget-pkgs` /
  Chocolatey community metadata (this doubles as the FMA-catalog replacement, §6 #12). Do **not** copy
  `ee/maintained-apps/`.
- **Build a shared `package-source` abstraction** (custom-pkg | URL/Ninite | winget | choco) with
  pluggable command templates, so all four share one MIT code path.
- **UI:** new tabs in `frontend/pages/SoftwarePage/SoftwareAddPage/`.
- **Effort:** M each (winget, choco, Ninite) on top of the §6 #2 prerequisite.

**Action1** is different — a **cloud control-plane integration**, not an executor (Action1's own agent
does the patching). Greenfield (no existing references).

- **Backend:** `server/integrations/action1/` — OAuth client-credentials client + a sync worker that
  pulls endpoint inventory and missing-patch/vuln status, and can trigger patch deployments via the
  Action1 REST API. Map Action1 endpoints ↔ Fleet hosts by hostname/serial.
- **Config:** `AppConfig.Integrations` (like Jira/Zendesk).
- **UI:** a new `PatchManagementPage` + per-host card showing Action1 patch status; deploy actions.
- **Effort:** XL (new client, sync, schema, dashboard). Keep it separate from the winget/Choco/Ninite
  work — don't conflate executor integrations with a control-plane integration.

**Optional native patching differentiator:** generalize `pkg/patch_policy/` (MIT) to join the CVE
engine output (`server/vulnerabilities/`) + installed-software inventory against winget/Choco
"latest version" metadata to compute "patch available" per host, then auto-queue an upgrade. Built
entirely on MIT components. Effort: L.

### 8.2 AV/EDR dashboards (Bitdefender GravityZone + Huntress)

Both are **read/monitor** integrations — poll the vendor API, normalize to a common AV/EDR status
model, attach to hosts, render a dashboard + host card. **100% MIT core, no `ee/`.**

- **Clients:** `server/service/externalsvc/bitdefender.go` + `huntress.go`.
- **Ingestion:** a self-contained bounded context `server/endpointprotection/` (clients + service +
  mysql + types, like `server/activity/`) **or** a cron in `server/cron/`.
- **Storage + host mapping:** new table keyed to `host_id` (vendor, agent version, last seen,
  protection state, last scan, open detections); resolve vendor endpoints → Fleet hosts with explicit
  precedence **hardware_serial → uuid → hostname**, and **persist unmatched records** so coverage gaps
  are visible. Add `Datastore` interface methods mirroring `SetOrUpdateMunkiInfo` (then run
  `go test ./server/service/` — uninitialized mocks crash other tests).
- **API:** `GET /hosts/{id}/endpoint-protection` + `GET /endpoint-protection` (aggregated).
- **Sync cron:** register in a **license-agnostic** group (every 15–30 min; page incrementally,
  respect lock/maxRunTime — GravityZone/Huntress have rate limits and can be slow at scale).
- **Frontend:** a top-level "Endpoint Protection" page (Overview / Bitdefender / Huntress tabs) using
  the existing `InfoCard` + table-config primitives; a host-detail card mirroring
  `frontend/pages/hosts/details/cards/MunkiIssues/`; a settings card with a "test connection"
  affordance.
- **Bonus:** register an AV/EDR-health **host vital** (`server/fleet/hosts.go hostVitals`) so hosts can
  be auto-labeled "unprotected"; optionally fire the existing host-status webhook when an agent goes
  unhealthy.
- **Effort:** L backend + L frontend.

> **Host-matching is the hardest correctness problem here** — hostname collisions, re-imaged
> machines, serial/UUID gaps. Design the precedence + unmatched-handling deliberately; it's the
> difference between a trustworthy dashboard and silent mis-attribution.

### 8.3 Android UEM rebuild

**Reframe: this is less "rebuild" and more "promote + harden."** The audit's biggest surprise is that
`server/mdm/android/` is a **complete, license-gate-free Android Enterprise (AMAPI) implementation** in
MIT core — per-device policies, enrollment tokens, fully-managed + BYOD, lock/wipe/passcode, app & web
app management, Pub/Sub notification handling.

Plan:

1. **Flip to the self-hosted direct-Google client.** `google_client.go` (`SignupURLsCreate`,
   `EnterprisesCreate`, `createPubSub`) already implements the direct path; it's gated behind the dev
   env var `FLEET_DEV_ANDROID_GOOGLE_CLIENT`. Make it the documented default with first-class config
   (GCP service-account JSON + project ID) in `server/config/config.go`. The `androidmgmt.Client`
   interface cleanly abstracts proxy-vs-direct, so this is selection + config, **not** new protocol
   code. **This is the single highest-leverage, lowest-effort win in the whole MDM area (M).**
2. **Stand up the GCP side:** an Android EMM/Pub/Sub registration under our own Google Cloud project
   (replaces `fleetdm.com/api/android/`).
3. **Verify team-scoping.** The base engine is MIT, but team-scoping of Android — like everything —
   rides the premium multi-tenancy layer, and the Android service is also injected into the ee
   constructor (`ee/server/service/service.go:38`). Don't assume Android is 100% free end-to-end;
   confirm which paths flow through `ee/` once Teams is rebuilt (§6 #1).
4. **Rebrand** the enrollment/agent touchpoints.

### 8.4 Mosyle MDM for iOS/macOS

Recommended v1: a **read/monitor integration that delegates Apple MDM to Mosyle** — which neatly
sidesteps the APNs/ABM cloud burden (§7).

- **New bounded context** `server/mdm/mosyle/` (mirror `server/mdm/android/`: types, client, service,
  datastore, poller). A Mosyle API client + a cron poller that ingests Mosyle device
  inventory/compliance and surfaces them as Fleet hosts **tagged `mdm source = mosyle`**.
- **Hook** at the host upsert path (`server/datastore/mysql/hosts.go`) reusing existing `host_mdm`
  tables; sync cron alongside `dep_syncer`/`reconcile_android_devices` in `cmd/fleet/cron.go`.
- **All Apple MDM actions (lock/wipe/profile push) go through Mosyle's API**, not Fleet's commander.
  Keep a clear per-host MDM-source distinction so native MDM commands are never issued to
  Mosyle-controlled devices.
- **Defer** any "Fleet-as-Apple-MDM-authority + hand off to Mosyle" model — it reintroduces the
  APNs/ABM dependency we're avoiding.
- **Effort:** L.

> **Decided (decision 2):** Mosyle is the **primary** Apple MDM authority (already stable for us), and
> the native Fleet Apple MDM stack is **kept as an alternative**. The two coexist via a clean per-host
> `mdm source` marker, so native-Fleet Apple devices and Mosyle-delegated devices can be managed side
> by side. This keeps §6 #3 (advanced native Apple MDM) and APNs vendor signing (§7) off the critical
> path — retained as an option, not a v1 requirement.
>
> **Setup:** see [`setup/mosyle.md`](./setup/mosyle.md) for obtaining the Mosyle API token and
> configuring the integration.

### 8.5 IPaaS onboarding/offboarding (future)

An external workflow system that talks to this platform via API. The audit defines a concrete
integration contract:

- **Auth:** one dedicated **api-only user** (`POST /api/latest/fleet/users/api_only`) with a scoped
  `APIEndpoints` whitelist (`server/api_endpoints/`); its session token is **non-expiring**
  (`sessions.go:912`). Use as `Authorization: Bearer`.
- **Eventing:** subscribe via the **Activities webhook**
  (`webhook_settings.activities_webhook.destination_url`) **and** poll `GET /activities` with
  `after=<last_id>` as a replay backstop.
- **Onboarding actions available *today* (zero rebuild):** create/transfer hosts, assign labels, push
  MDM config profiles (`POST /configuration_profiles`), enqueue MDM commands (`POST /commands/run`),
  run host-scoped scripts (`POST /scripts/run`), manage operator users/invites, global enroll secrets.
- **Offboarding:** `DELETE` user/invite; Android wipe today; full lock/wipe once §6 #4 lands.
- **Gaps to build for a production IPaaS:**
  - Onboarding-by-team needs **Teams** (§6 #1, XL).
  - "Push software during onboarding" needs the **software pipeline** (§6 #2, XL).
  - **Harden the webhook (S):** add HMAC signature + stable event id to
    `server/activity/internal/service/new_activity.go` (today: single URL, unsigned, in-process retry
    only). Optionally build a real multi-destination/filtered subscription system (M).
- **Use core Labels (incl. dynamic smart labels) as the interim grouping primitive** until Teams is
  rebuilt — but document that labels do **not** provide RBAC isolation.

---

## 9. Where integration code should live

**You asked whether a `human/` folder or a `plugins/` folder is more appropriate. Answer: `human/`
for docs, and first-class MIT packages under `server/` for code — *not* a "plugins" folder, because
Fleet has no plugin architecture.** Integrations are compiled into the binary as ordinary Go packages
(that's how Jira/Zendesk/Calendar/Android all work). A `plugins/` folder would imply a runtime
extension system that doesn't exist and would fight the codebase.

> **Refinement (see [PLUGINS.md](./PLUGINS.md)):** we *are* building an extension-point architecture —
> a **compile-time provider registry** (`HostStatusProvider`, `IntegrationProvider`, `RouteRegistrar`,
> `CronRegistrar`) that generalizes seams Fleet already has (`EnterpriseOverrides`, the Munki
> host-status pattern, the `HandlerRoutesFunc` slice). That is *not* a runtime `plugins/` folder and
> doesn't change the rule above: provider code still lives in first-class MIT packages under `server/`;
> the registry is wiring only. A thin slice (the `HostStatusProvider` interface) is our upstream-PR
> candidate.

Recommended structure:

```
human/                              ← our company namespace: strategy, ADRs, fork-specific docs
  OSS.md                            ← this document
  README.md                         ← what this folder is
  (later) adr/, runbooks/, clean-room-log.md

server/integrations/               ← NEW: our external-API integrations (all MIT)
  action1/                         ← patch-management control plane
  bitdefender/                     ← GravityZone AV/EDR ingestion
  huntress/                        ← Huntress EDR ingestion
server/endpointprotection/         ← NEW bounded context: normalized AV/EDR status + datastore
server/mdm/mosyle/                 ← NEW bounded context: Mosyle device ingestion
server/licensing/                  ← clean-room license validator (replaces ee/server/licensing)

# Rebuilt premium features go in their natural core homes (fill the ErrMissingLicense stubs),
# e.g. server/service/teams.go, software_installers.go, scripts.go — NOT under human/ or plugins/.
```

This keeps our code idiomatic (the `go-reviewer` agent and linters expect packages in these
locations), keeps a clean licensing story (everything we author is MIT under `server/`), and reserves
`human/` for the strategy/ADR layer.

---

## 10. Recommended phasing / roadmap

**Phase 0 — Clean fork that compiles & ships (foundation).**
Delete `ee/` + `docs/`; sever all 11 `ee/` imports (§2.3 step 3); clean-room license validator
(stub → premium); rebrand; self-host TUF + CVE feeds; APNs upload-only. → *A pure-MIT, single-tier
UEM binary you can legally sell.* Effort: M–L.

**Phase 1 — Light up the free wins.**
Flip the Style-A gates: CVSS/EPSS/CISA vuln scores, audit-log streaming, MFA, label-scoped
queries/policies. Promote Android direct-Google client to default (§8.3). Ship failing-policy
webhooks/tickets. → *A genuinely competitive monitoring + Android UEM + vuln product.* Effort: S–M.

**Phase 2 — AV/EDR + patch dashboards (our differentiators, all greenfield MIT).**
Bitdefender + Huntress dashboards (§8.2); Action1 control plane (§8.1); winget/Choco/Ninite once the
software pipeline exists. Mosyle ingestion (§8.4). These are independent and parallelizable.

**Phase 3 — Multi-tenancy + advanced MDM (the heavy clean-room rebuilds).**
Teams/Fleets (§6 #1) → unlocks team-scoped everything and GitOps; software deployment pipeline
(§6 #2); host lock/unlock/wipe (§6 #4); advanced MDM enforcement (§6 #3) *if* we're the Apple MDM
authority rather than delegating to Mosyle; RBAC, SCIM, conditional access as demand justifies.

**Phase 4 — IPaaS.** Build the external workflow system against the (now complete) API; harden the
webhook; add Teams-based onboarding.

---

## 11. Risks & open questions

**Risks**

- **Clean-room discipline** is the top legal risk. `ee/` may be *read to understand contracts* (the
  signatures are echoed in MIT core stubs) but **never copy-translated**. Maintain a written
  clean-room log. Get counsel sign-off on the process.
- **Trademark** — rebrand fully before any commercial distribution; the scrub includes error-message
  constants and the license header, not just logos.
- **"Routes exist" ≠ "feature works."** Every `/fleets/*`, `/software/package`, `/hosts/{id}/lock`
  route is registered in core but returns 402 until rebuilt. A naive fork compiles and serves broken
  flows. Keep frontend tier flags aligned with which server methods are actually rebuilt.
- **Multi-tenancy is load-bearing.** Rebuild Teams first and carefully; partial RBAC un-gating can
  create privilege-escalation gaps (security-review `server/authz/policy.rego`).
- **Removing license checks individually is error-prone** — some core methods mix free and premium
  branches in one function (`windows_mdm_profiles.go`, `vulnerabilities.go:97`). Audit each
  `IsPremium`/`ErrMissingLicense` branch; don't bulk-delete.
- **AV/EDR host-matching** mis-attribution; **API rate limits** on GravityZone/Huntress/Action1 at
  scale.
- **APNs vendor signing** is the one genuinely hard external dependency for native Apple MDM —
  mitigated by upload-only or by delegating Apple to Mosyle.

**Decisions of record** (2026-07, from the team)

1. **Tiering — single-tier now, keep the door open.** Ship single-tier (license validator reports
   premium unconditionally, behind a config flag) for **internal dogfooding first**. **Preserve the
   tiering machinery**: do **not** rip out the `license.IsPremium(ctx)` gates or the frontend
   `isPremiumTier`/`PremiumRoutes` machinery, and do **not** "inline premium logic into core." Keep
   the `EnterpriseOverrides` seam and the gate structure so that **re-enabling paid tiers later (if we
   productize) is a config/validator change, not a re-architecture.** This overrides the MDM agent's
   "delete the stubs, inline to core" suggestion.
2. **Apple strategy — Mosyle primary, native MDM kept as an alternative.** Use **Mosyle as the primary
   Apple (iOS/iPadOS/macOS) MDM authority** (it already works and is stable for us) via the read/ingest
   design in §8.4. **Keep the native Fleet Apple MDM stack available as an alternative** — the two
   coexist via a per-host `mdm source` marker. This means §6 #3 (advanced native Apple MDM) is *not*
   on the critical path but is retained as an option; APNs vendor signing (§7) only matters for the
   native path.
3. **Multi-tenancy — keep it.** We manage **multiple clients**, so the Teams/Fleets rebuild (§6 #1,
   XL) stays in scope (Phase 3). Labels are the interim grouping primitive until it lands.
4. **ChromeOS — deferred.** Skip for v1. Revisit later (Chromebooks are used as secure remote-desktop
   endpoints we'll want to manage). `ee/fleetd-chrome` is MIT-via-carve-out; relocate + counsel
   sign-off when we pick it back up.
5. **CVE feeds — confirmed OK** for our use (EPSS/CISA-KEV).
6. **Clean-room is mandatory and enforced.** Agents/engineers reimplementing `ee/` features must
   **never read `ee/` source** — they work only from feature descriptions + the public `fleet.Service`
   interface. See **[clean-room-protocol.md](./clean-room-protocol.md)** and the running log in
   `clean-room-log.md`.
7. **Bitdefender** integrates via the **GravityZone API/SDK**; **Action1** via **instance-ID MSI push +
   REST API**. Per-vendor setup docs live in **[`setup/`](./setup/)**.

**Still open (smaller):**

- Exact host-matching precedence per vendor (serial vs. UUID vs. hostname) — decide during §8.2/§8.4 build.
- Whether the native Apple MDM alternative (decision 2) is ever exposed to clients or stays internal-only.

---

## Appendix A: key file map

**Licensing / gating seam**
- `LICENSE`, `ee/LICENSE`, `ee/README.md` — the license terms
- `ee/server/licensing/licensing.go` + `pubkey.pem` — proprietary JWT validator (replace)
- `server/contexts/license/license.go` — `IsPremium(ctx)` (MIT, the gate everyone calls)
- `server/fleet/app.go:1746` — `LicenseInfo` / `IsPremium()` / tier consts (MIT)
- `server/fleet/service.go:21` — `EnterpriseOverrides` struct (the premium hook list)
- `ee/server/service/service.go` — the premium wrapper (the rebuild template, do not copy)
- `cmd/fleet/serve.go:577,1276` — `eeservice.NewService` wiring + `initLicense` seam
- `server/fleet/errors.go:17` — `ErrMissingLicense` → HTTP 402

**MDM**
- `server/mdm/nanomdm/`, `nanodep/`, `scep/` — vendored MIT Apple MDM protocol
- `server/service/apple_mdm.go`, `server/mdm/apple/{commander,cert,apple_mdm}.go`
- `server/service/microsoft_mdm.go`, `server/mdm/microsoft/wstep.go` — Windows MDM (MIT)
- `server/mdm/android/` + `service/androidmgmt/{client,google_client,proxy_client}.go` — Android (MIT)
- `ee/server/service/{mdm,apple_mdm,vpp,setup_experience}.go` — premium MDM (rebuild)

**Software / patch**
- `server/service/{software_installers,scripts,maintained_apps}.go` (core stubs)
- `ee/server/service/software_installers.go` (3912 lines, rebuild)
- `orbit/pkg/installer/installer.go`, `orbit/pkg/scripts/exec_windows.go` (MIT executor)
- `pkg/patch_policy/`, `server/mdm/maintainedapps/sync.go`

**Integrations / extension points**
- `server/fleet/integrations.go`, `server/service/externalsvc/`, `server/service/appconfig.go`
- `cmd/fleet/cron_registration.go`, `server/service/schedule/`
- `server/fleet/hosts.go` (MacadminsData/Munki precedent), `server/service/handler.go`
- `frontend/router/paths.ts`, `frontend/router/index.tsx`, `frontend/components/top_nav/SiteTopNav/navItems.ts`
- `frontend/pages/hosts/details/cards/MunkiIssues/` (host-card pattern)

**API / vuln**
- `server/service/handler.go`, `server/api_endpoints/`, `server/service/sessions.go`
- `server/activity/internal/service/new_activity.go` (activities webhook)
- `server/vulnerabilities/` (full CVE engine), `server/vulnerabilities/nvd/sync.go:316` (score gate)

---

## Appendix B: how this analysis was produced

A multi-agent audit fanned out across seven dimensions (licensing, OSS/EE feature split, MDM,
software/patch, integrations/dashboards, API/extensibility, osquery/vuln), each producing
evidence-backed findings with real file paths. The two highest-stakes dimensions — **licensing** and
the **feature split** — were then put through an **adversarial verification pass** that independently
re-read the files and corrected the first pass. Corrections folded into this document:

- The OSS binary's `ee/` import coupling was **undercounted** — it's **9** imports in `serve.go`
  (incl. `ee/server/scim`) and **2** in `cron.go` (incl. `ee/server/webhooks`). Reflected in §2.3.
- **MFA** was mislabeled enterprise-ee; it's actually **partial** (machinery is MIT, one enable-gate).
  Reflected in §4.
- **Host lock/unlock/wipe** are a *separate* premium surface in `scripts.go`, not part of "free
  scripts." Called out in §4 and §6 #4.
- **Android MDM** was omitted from the first feature inventory — it's a **major MIT-core win**.
  Elevated in §1, §4, §8.3.
- `fleetd-chrome`'s MIT status is **carve-out-dependent** (relocate + counsel). §2.1.
- The rebrand scrub is **larger** than a few constants (`fleetdm.com` URLs pervade error strings). §2.3.

*Re-verify any specific claim against the cited file before relying on it for implementation — line
numbers drift as `main` moves.*
