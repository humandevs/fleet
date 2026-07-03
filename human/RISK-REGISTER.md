# RISK-REGISTER.md — red-team of the whole design

> Prioritized risk register from an adversarial red-team (security / reliability / compliance-ops /
> completeness lenses) that **read the actual Fleet code**, not just our docs. Several findings are
> code-verified and worse than the planning docs assumed. Companion to all `human/` docs.
> **Read the "decide now" section first — those are foundational calls that are expensive-to-impossible
> to reverse once co-mingled production data accrues.**

## Executive summary

This is **one shared control plane** that holds run-as-SYSTEM/root + lock/wipe over every device in every
client tenant **and** custodies every client's vendor keys + MDM/APNs certs — yet its tenant boundary is a
soft, being-rebuilt-clean-room RBAC/Teams layer over a **single DB, single global config, single crypto
key.** All four lenses independently converged on the same #1: **the instance/tenant-isolation model is
being decided by omission**, and nearly every other severe risk (secrets-at-rest, offboarding, DR blast
radius, residency, cross-tenant leaks) is downstream of it. Two failure classes make the flagship
coverage-matrix differentiator *actively harmful* rather than merely absent: **false-green on stale data**
and **host mis-attribution across tenants**. Around those sit catastrophic-blast-radius primitives that are
cheap to bound now and near-impossible later.

## 1. Decide now — foundational calls (before building more)

1. **INSTANCE / TENANT-ISOLATION MODEL** 🚨 — explicitly cost & choose: shared-DB-with-Teams vs
   schema/DB-per-tenant vs **instance-per-client**. At ~1000 endpoints across a handful of clients,
   per-instance may be **cheaper and far safer** than the XL clean-room Teams rebuild it would *eliminate*,
   and it caps blast radius for secrets, offboarding, residency, DSARs, evidence exports. **The docs picked
   "single shared instance" silently. Make it an explicit ADR before any Phase-3 work.** Reversing a shared
   multi-tenant DB later is the most expensive change in the program.
2. **SECRETS & KEY MANAGEMENT** 🚨 — stop mirroring the mask-only Jira pattern (vendor creds are plaintext
   in `app_config_json`). Envelope-encrypt **every** secret in a dedicated table with KMS/HSM-backed
   **per-tenant** data keys; partition integration creds per-tenant (not the global `AppConfig.Integrations`
   singleton); **never store the Mosyle admin password** (token-only); escrow + rotate `server_private_key`
   separately from the DB blast radius.
3. **COVERAGE FRESHNESS CONTRACT** — make staleness first-class: the **read path** ages each cell against a
   per-category TTL and force-renders stale/unknown regardless of stored state, with per-cell last-updated +
   collector-liveness alerting. (The `*_updated_at` columns already exist — wire them in.)
4. **ONE tenant-scoped IDENTITY-RESOLUTION service** every collector must call — single precedence
   (serial→uuid→hostname, hostname never alone), team-scoped (never match across tenants), confidence-scored
   with refuse-to-match, first-class unmatched queue. **Do NOT reuse `HostByIdentifier`** (verified
   un-scoped, nondeterministic).
5. **DESTRUCTIVE-MASS-ACTION GUARDRAILS** as an authz-layer primitive — WebAuthn step-up + independent
   second-approver above a small host threshold, per-tenant + global fan-out rate limits + circuit breaker,
   audited break-glass, tamper-evident off-box audit.
6. **AI-INSTALLER + PLUGIN TRUST MODEL** — AI replay only in ephemeral locked-down VMs, signed/pinned
   recordings, allowlisted verifiable action vocabulary, on-screen text = untrusted data, no cross-tenant
   reuse; WASM-only default, gRPC default-deny + OS-confined, HSM signing key + revocation/kill-switch.
7. **TIER-0 INFRA & HA/DR** — multi-AZ MySQL + HA Redis, automated encrypted backups + tested PITR/DR
   (RTO/RPO + restore drills), KMS master key outside backup blast radius, leader-election / `SKIP LOCKED`
   so >1 replica runs safely (the worker is verified single-instance-only), a break-glass ingress that
   doesn't depend on Cloudflare Access, and ≥1 real alerting channel.
8. **TUF STAGED ROLLOUT** — canary → health-gated slow ramp per tenant with auto-halt on check-in drop →
   signed last-known-good one-flip rollback + agent watchdog. orbit runs as SYSTEM and is the remediation
   path, so one bad target is a cross-tenant extinction event.
9. **IMMUTABLE, PER-TENANT, RETAINED (≥1yr) AUDIT SINK** from day one for all privileged actions. The
   activities feed is mutable and auto-deletes on a rolling window; history can't be recreated later.
   Also: **don't accumulate co-mingled multi-tenant data in Phases 0–2 and retrofit isolation in Phase 3** —
   keep Phases 0–2 strictly internal, or pull a minimal tenant boundary forward before a second client lands.

## 2. Ranked risk register

Legend — Sleeping: **✗ fully** (unaddressed) · **~ partial** · Sev: 🔴 critical · 🟠 high · 🟡 medium.

| # | Risk | Sev | Sleeping | Mitigation (short) |
|---|------|-----|----------|--------------------|
| 1 | Tenant isolation is a soft RBAC boundary (`whereFilterHostsByTeams` returns `TRUE` for any global role); one missing predicate/rego bug = breach of ALL clients | 🔴 | ~ | ADR on per-instance vs shared; row-level tenant scoping; forbid standing global roles; cross-tenant negative-auth suite on every commit |
| 2 | Coverage matrix shows **green on stale data** (write cron stops silently, read path has no TTL) | 🔴 | ~ | Read-path TTL per category → force stale/unknown; per-cell last-updated; per-source heartbeat + alert |
| 3 | Integration secrets **plaintext at rest** (`SaveAppConfig`), shared single `server_private_key`, Mosyle admin pw stored | 🔴 | ~ | Dedicated secrets table, envelope-encrypt w/ per-tenant KMS DEKs; token-only Mosyle; rotate/escrow key; CI test: no plaintext secret |
| 4 | **Host mis-attribution across tenants** (`HostByIdentifier` un-scoped/nondeterministic; global Integrations singleton can't hold per-client creds) | 🔴 | ~ | One tenant-scoped resolver; store vendor device-id for sticky re-link; per-team creds + vendor-org↔team binding |
| 5 | **Zero guardrails on destructive fan-out** — one token wipes/scripts thousands across all tenants | 🔴 | ✗ | Step-up + second-approver above threshold; per-tenant/global rate cap + circuit breaker; break-glass; off-box audit; MSA/insurance/IR runbook |
| 6 | **AI GUI-replay agent = admin RCE by construction**, un-threat-modeled (prompt-injection, replay drift, tampered recording) | 🔴 | ✗ | Ephemeral VM only, signed recordings, allowlisted actions, screen-text-as-data, per-step success check + human approval; prefer silent installs |
| 7 | **Bad orbit TUF update bricks every device** in every tenant; no canary/rollback | 🔴 | ✗ | Canary → health-gated ramp → signed last-known-good rollback + agent watchdog; monitor TUF metadata expiry |
| 8 | Platform **DB/Redis is tier-0 SPOF** (all keys/certs/escrow) with no HA/backup/PITR/DR stated for OUR platform | 🔴 | ✗ | Managed multi-AZ MySQL + HA Redis, encrypted backups + PITR, tested restore, RTO/RPO, KMS key outside backups |
| 9 | **No immutable audit** — activities feed auto-deletes; SIEM streaming treated as premium, not WORM | 🟠 | ✗ | WORM/append-only per-tenant ≥1yr sink from day one for all privileged actions |
| 10 | **Insider/phished global admin = god-mode**; OSS.md touts **non-expiring** api-only tokens (standing all-tenant backdoor) | 🟠 | ~ | No standing global admin; JIT/break-glass elevation; rotating scoped expiring tokens + revocation; SSO deprovisioning; hardware MFA |
| 11 | **Forgeable coverage inputs** — most push receivers authed by shared-secret/`?token=` (only Huntress uses real HMAC); agent self-report over team-wide enroll secrets | 🟠 | ~ | HMAC-signed bodies + constant-time compare + anti-replay everywhere; prefer central pollers for security columns; flag vendor-vs-self disagreement |
| 12 | **Plugin trust** — gRPC plugins inherit server privileges unless OS-confined; in-process providers unsandboxed (panic/hang crashes the god-server); signing infra open; README/§9 contradicts the RFC | 🟠 | ~ | gRPC default-deny + OS confinement as load precondition; supervise in-process providers (timeout/panic-recover→unknown); HSM signing + revocation; fix doc contradiction |
| 13 | **Cloudflare = single trust+availability chokepoint**; Bypass-before-Enforce ordering can serve new admin routes unauth; vendor push receivers sit under Enforce (Access 302s them → silent degrade); no break-glass | 🟠 | ~ | Enforce-by-default + minimal device Bypass allowlist (fail closed); receivers on allowlist w/ HMAC; Tunnel-only origin; break-glass ingress; staged WAF on canary; test every admin route requires Access |
| 14 | **Nobody watches the watchers** — no self-monitoring/alerting; no cert/token expiry tracking (APNs renews annually, fails silently fleet-wide); Fleet has no Slack/Teams sink | 🟠 | ✗ | Per-source heartbeat metrics + staleness/zero-write alerts; single expiry registry (30/14/7/1d); ship a notification sink or Prometheus+Alertmanager |
| 15 | **No client-tenant lifecycle / residency** — 2nd isolated client blocked pre-Teams; offboarding (purge scattered rows, certified destruction) undesigned; no BAA/DSAR/residency for GDPR/CMMC | 🟠 | ✗ | Templated idempotent audited tenant provisioning + teardown runbook; decide residency/subprocessor/DPA up front (per-instance makes this trivial) |
| 16 | **Mosyle-as-Apple-authority has no failover** — if Mosyle is down you can't wipe a lost Mac; native MDM isn't hot failover; v1 delegated wipe is "optional" | 🟠 | ~ | Make delegated Mosyle lock/wipe a first-class monitored MVP requirement + incident fallback; reconsider native Apple MDM for lock/wipe; monitor APNs/Mosyle expiry |
| 17 | **App-tier HA conflicts with single-instance worker** (`worker.go:79` not concurrent-safe, no `SKIP LOCKED`) — 2 replicas double-fire collectors → 429 bans → stale coverage | 🟠 | ~ | Leader election (Redis/k8s lease) for the collector role or add `FOR UPDATE SKIP LOCKED`; Redis GCRA per-provider governor + 429 breaker |
| 18 | **Collector schema-drift silently zeroes a column** — all load-bearing JSON field names are UNVERIFIED; no contract/e2e tests → false-green in prod | 🟠 | ~ | Per-provider contract tests from captured responses in CI; strict deserialization (error on missing field → loud stale); alert on "sync ok but 0 rows"; e2e smoke per collector |
| 19 | **Fork-maintenance severely under-scoped** — ~10k+ lines of security-critical clean-room rebuilds maintained FOREVER in the exact core files upstream keeps editing; **clean-room bars cherry-picking upstream's own security fixes** (every upstream premium CVE must be re-derived) | 🟠 | ~ | Re-cost vs alternatives (license EE; stay upstream; per-client instances that avoid the Teams rebuild); minimize rebuilt-in-core surface; track upstream EE CHANGELOG/CVEs; migration-namespacing day one |
| 20 | **"Always-premium" flag flips authz-sensitive gates at once** (custom roles/RBAC) → fail-OPEN on half-rebuilt enforcement = privilege escalation, not just nil-panics | 🟠 | ~ | Keep authz/destructive gates independent of the blanket flag; Teams/RBAC deny-by-default + passing negative-auth suite before exposing role UI |
| 21 | **Team-wide non-expiring enroll secrets** baked into installers + live secrets (WARP token, VPN certs) pushed to any team-enrolled host → leaked secret enrolls a rogue host that receives the pushed creds & pivots | 🟡 | ~ | Short-lived rotatable per-site enroll secrets; per-device attestation (SCEP challenge) not a shared secret; scope secret-bearing profiles to attested devices; anomaly-alert on enrollments |
| 22 | **Provisioning/host lifecycle non-idempotent** — half-installs report success; re-image/rename forks a duplicate host with a stale "protected" ghost that never turns red | 🟡 | ~ | Idempotency + independent success-check contract; resumable provisioning state machine; reconciliation pass per sync; key joins on immutable hardware ids |
| 23 | **Per-tenant evidence exports / billing-metering / DSAR undesigned** — export from shared DB risks leaking other tenants' rows; mutable host table auto-prunes billable records; unbounded activities/host_software taxes the hot ListHosts JOIN | 🟡 | ✗ | Centrally-filtered signed per-tenant exports w/ tests; metering source-of-truth (immutable enroll ledger); DSAR tooling + processor/controller split; retention/partition for activities & host_software |

## 3. Quick wins (cheap, high-value, do early)

1. **Wire the existing `*_updated_at` columns into the coverage read path** with a per-category TTL → stale cells render "unknown"; per-cell tooltip + "stale collectors" banner. Kills the flagship false-green.
2. **Constant-time HMAC (Huntress/Svix model) on every webhook receiver** + timestamp/nonce anti-replay; move Android Pub/Sub token out of the URL into a header.
3. **Add all vendor push/webhook receivers to the Cloudflare device-plane Bypass allowlist** (else Access 302s the vendor and push silently degrades to polling).
4. **Strict deserialization** (error on missing required field) → schema drift fails LOUD into stale/unknown, not false-green zeros; alert on "sync ok but 0 rows."
5. **Cert/token/key expiry registry** with 30/14/7/1-day alerts (APNs, SCEP/Win CA, CF tokens, vendor keys, Mosyle JWT, plugin signing key, TUF timestamp).
6. **CI test: no secret field persisted unencrypted** + a **cross-tenant negative-authorization suite** on every commit.
7. **Never store the Mosyle admin password** — token-only (Bearer) via a least-privilege, IP-pinned admin whose secret lives only in the KMS.
8. **Leader-election / `SELECT … FOR UPDATE SKIP LOCKED`** on job claiming + per-provider 429 circuit breaker.
9. **Ship one real notification sink** (Slack/Teams/generic webhook, or `/metrics` → Prometheus+Alertmanager) so alerts can page before GA.
10. **Invert Cloudflare Access to enforce-by-default** + minimal device-Bypass allowlist (fail closed) + a test asserting every admin route requires Access. Fix the README/§9-vs-RFC contradiction.
11. **Adopt a fork migration-namespacing/bump convention** day one (the repo ships `/bump-migration` because rebase collisions are a known recurring pain).

## 4. What my earlier top-8 missed (author-list critique)

The critic credited the top-8 as a competent *slice-level* analysis but structurally incomplete in three ways:
- **Decided the biggest thing by omission:** single shared instance vs per-client instance was never weighed — and that silent choice *manufactures* the XL Teams/RBAC rebuild, the skeleton-key DB, and a long unisolated Phase 0–2. Per-instance may be cheaper *and* safer at this scale.
- **Omitted three whole domains:** per-tenant integration creds + vendor-org→client mapping (the global `AppConfig.Integrations` singleton can't represent them, making host-matching a cross-tenant confidentiality bug); the AI-installer threat model; and DR + key management for a DB whose loss bricks all fleets / whose leak is a total breach.
- **Mis-ranked named risks:** fork-maintenance framed as bounded one-time ports rather than a **forever parallel reimplementation in the exact core files upstream keeps editing, with clean-room barring cherry-picking upstream's security fixes**; Mosyle's decision *removed* the APNs "hardest dependency" but *added* an un-ranked Mosyle-availability dependency (the risk moved, the ranking didn't); wazero runtime-escape dismissed as "residual" despite god-level host privilege. Plus internal contradictions: MVP says team-scoping "needs no new work" while OSS ties it to the XL Teams rebuild; README/§9 says "no runtime plugins" while the RFC builds a WASM/gRPC loader.

## 5. Verified code receipts (why this is credible, not hand-waving)

- Secrets plaintext at rest: `server/datastore/mysql/app_configs.go:80-97` (`SaveAppConfig` json.Marshals, no encryption); `MaskedPassword` presentation-only `server/fleet/app.go:808`; `server_private_key` encrypts only `mdm_config_assets` (`mdm.go:213`), not AppConfig.
- Tenant isolation = one WHERE clause: `whereFilterHostsByTeams` returns `TRUE` for global admin/maintainer/technician/observer+ (`server/datastore/mysql/…:870`).
- Host matcher un-scoped/nondeterministic: `HostByIdentifier` = 5-column `IN` with `LIMIT 1`, no `ORDER BY`, no team filter.
- Worker not concurrent-safe: `server/worker/worker.go:79`; job claim has no `SKIP LOCKED`.
- Activities auto-delete: `CleanupExpiredActivities` on a rolling `expiryWindowDays`.
- orbit runs scripts as SYSTEM: `orbit/pkg/scripts/exec_windows.go` (`powershell -ExecutionPolicy Bypass`).

---

*Source: red-team workflow `wbknx0uep` (security / reliability / compliance-ops / completeness lenses +
synthesis, 5 agents, code-verified).*
