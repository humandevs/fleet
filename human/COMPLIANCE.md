# COMPLIANCE.md — SOC 2 / HIPAA / CMMC (800-171) / GDPR control map

> What each framework requires **in source** vs. infra vs. operational, for an instance-per-MSP UEM/RMM
> that runs as SYSTEM on client devices. Companion to [RISK-REGISTER.md](./RISK-REGISTER.md),
> [ZERO-TRUST.md](./ZERO-TRUST.md), [FORK-STRATEGY.md](./FORK-STRATEGY.md). Code-verified.

## The framing that matters

**SOC 2 is ~70% operational.** The report attests to *your company's* controls over 3–12 months, not to
the product's code. The product's job is to **emit evidence** (audit trails, access reviews, change
records) — not to "be compliant." Only three Common-Criteria families are meaningfully product-backed:
**CC6** (logical access — MFA, RBAC, encryption), **CC7** (monitoring, audit, IR), **CC8** (change mgmt).
Where source-level features actually bite is **HIPAA §164.312**, **NIST 800-171 (AC/AU/IA/SC/MP)**, and
**GDPR Art.32** — and **instance-per-MSP is the single largest compliance lever** (below).

## Framework → control families that touch a SYSTEM-power UEM/RMM

- **SOC 2 (AICPA TSC):** CC1–CC9 + optional A1/C1/PI1/P. Product-backed = CC6/CC7/CC8; A1 = HA/DR (infra);
  CC1–3/5/9 = policy/process.
- **HIPAA Security Rule:** you + the MSP are Business Associates. §164.308/310 = admin/physical (ops/infra);
  **§164.312 technical** = the core: access control (unique ID *required*, break-glass *required*, auto-logoff/
  encryption *addressable*), audit controls, integrity, person authentication (MFA), transmission security.
  \+ BAAs (§164.314) + 72h breach notice.
- **CMMC 2.0 L2 = full NIST SP 800-171:** UEM families AC, AU, IA (MFA=3.5.3), **SC** (encryption in transit
  *and* at rest; **FIPS-validated crypto SC.L2-3.13.11 is a hard requirement**), CM, SI, IR, MP (disposal
  3.8.x). **Residency:** DFARS 252.204-7012 → CUI cloud must meet FedRAMP Moderate/equivalency; sovereignty
  language → **US-persons-only admin** (GCC-High/GovCloud pattern) — infra + operational.
- **GDPR:** Art.5/28(DPA — you're the MSP's sub-processor)/30/32/33-34(72h)/15-22(DSAR)/44-49(residency)/
  17(erasure). Mostly operational + infra, with product hooks for DSAR export + erasure.

## Control → implementation matrix

Class: **PRODUCT** (build/config in the app) · **INFRA** · **OPERATIONAL** (policy/process, not source).

| Control theme | SOC2 / HIPAA / 800-171 / GDPR | Class | Upstreamability | Where (file:line / infra / policy) |
|---|---|---|---|---|
| SSO + federated MFA (phishing-resistant) | CC6.1 / §164.312(d) / IA.3.5.3 / Art.32 | PRODUCT+INFRA | free MIT | `server/sso/` (free) → IdP MFA + Cloudflare Access/WARP. **Don't rely on built-in email MFA** (`sessions.go makeMFAEmail`, gate `users.go:715`) |
| Session timeout / auto-logoff | CC6.1 / §164.312(a)(2)(iii) / AC.3.1.11 | PRODUCT | config | session TTL; **harden non-expiring API tokens** `sessions.go:912` (RISK #10) |
| Unique user ID + break-glass | CC6.1 / §164.312(a)(2)(i)(ii) / AC | PRODUCT+OPS | free + fork | Fleet users unique; break-glass ingress + audited elevation is fork/infra (RISK #5/#13) |
| RBAC / custom roles / least-privilege | CC6.3 / §164.308(a)(4) / AC.3.1.1-.5 | PRODUCT | fork rebuild | authz `server/authz/policy.rego` (MIT); roles gate `users.go:149`; deny-by-default + negative-auth suite (RISK #1/#20) |
| Tenant segregation (MSP boundary) | CC6.1 / §164.308 / AC / Art.32 | INFRA (arch) | fork decision | **INSTANCE-PER-MSP** (hard); Teams = soft isolation within an MSP |
| Destructive-action separation of duties | CC6.1/CC7.2 / §164.312(c) / AC | PRODUCT | fork (partly RFC) | WebAuthn step-up + 2nd approver + rate-limit/breaker (RISK #5) |
| Encryption in transit | CC6.7 / §164.312(e) / SC.3.13.8 / Art.32 | INFRA | build-time (FIPS) | TLS + cloudflared tunnel; **FIPS 140 (SC.3.13.11) = Go FIPS build variant** |
| Encryption at rest (DB/volume) | CC6.1 / §164.312(a)(2)(iv) / SC.3.13.16 / Art.32 | INFRA | n/a | RDS/EBS + per-instance KMS CMK |
| Secret/credential encryption (app-level) | CC6.1 / §164.312 / SC / Art.32 | PRODUCT | RFC-to-core (partial) + fork | seam `server/mdm/mdm.go:213 EncryptAndEncode`; **gap `app_configs.go SaveAppConfig` = plaintext** (RISK #3); secrets table + per-tenant KMS DEK |
| Key management / rotation / custody | CC6.1 / §164.312(a)(2)(iv) / SC.3.13.10 | INFRA+PRODUCT | fork | move `server_private_key` to KMS; rotate/escrow outside DB blast radius |
| Immutable audit logging | CC7.2 / §164.312(b) / AU.3.3.x / Art.30 | PRODUCT+INFRA | RFC-to-core candidate | stream via gate `cmd/fleet/logging.go:75` → `server/logging/` → WORM (S3 Object Lock). **Feed is hard-DELETED** `activity/internal/mysql/activity.go:200` (RISK #9) |
| Audit retention ≥1yr (HIPAA 6yr) | CC7.2 / §164.316(b)(2) / AU.3.3.1 | INFRA+OPS | config+infra | WORM retention; `ActivityExpiryWindow` config |
| Change management | CC8.1 / §164.308(a)(8) / CM.3.4.x | PRODUCT+OPS | free CLI | GitOps `cmd/fleetctl/` (config-as-code = change record); approval policy operational |
| Agent-update change control | CC8.1 / CM / SI | PRODUCT+INFRA | fork | TUF canary → health-gated ramp → signed rollback (RISK #7/#8) |
| Monitoring / alerting | CC7.1-.3 / §164.308(a)(1)(ii)(D) / SI.3.14.x | PRODUCT+INFRA | fork (greenfield) | heartbeats + expiry registry + notification sink (RISK #14); `/metrics` exists |
| Incident response | CC7.3-.5 / §164.308(a)(6) / IR.3.6.x / Art.33 | OPERATIONAL | n/a | IR runbook incl. destructive-action break-glass; 72h breach notice |
| Retention & disposal / sanitization | CC6.5 / §164.310(d) / MP.3.8.x / Art.5/17 | PRODUCT+INFRA+OPS | fork tooling | **crypto-shred = destroy instance + KMS key**; table partition/retention (RISK #15/#23) |
| Tenant offboarding / erasure (DSAR) | CC6.5 / MP / Art.15/17/28 | PRODUCT+OPS | fork | templated teardown; per-instance export can't leak cross-MSP (RISK #15/#23) |
| Data residency | DFARS-7012 CONUS / Art.44 | INFRA+OPS | n/a | region-pin per instance; US-persons-only admin via Access posture |
| Vendor / subprocessor + BAA/DPA | CC9.2 / §164.314 / SA / Art.28 | OPERATIONAL | n/a | subprocessor register = **OSS.md §7 hosted-deps list**; self-hosting TUF/CVE feeds *removes* subprocessors |
| HA / DR / backup / restore-test | A1.2 / §164.308(a)(7) / Art.32(1)(c) | INFRA | n/a | multi-AZ MySQL + HA Redis, encrypted backups + tested PITR, KMS key outside backups (RISK #8) |
| Governance / risk / training | CC1-5,9 / §164.308(a) / RA,CA,AT,PS | OPERATIONAL | n/a | policies, access reviews, risk register, training — **not source** (SOC2 ~70% here) |

## Instance-per-MSP is the load-bearing compliance control

It converts four otherwise-hard problems into bounded infra guarantees:
- **Segregation** — physical/DB isolation between MSPs eliminates the soft-RBAC cross-tenant breach class
  (RISK #1: `whereFilterHostsByTeams` returns `TRUE` for any global role). A rego/predicate bug can breach
  only *that MSP's* own client Teams, never all MSPs. Far more defensible to an auditor.
- **Residency** — region-pin per instance (GovCloud/CONUS for CUI, EU for GDPR); no shared-DB contradiction.
- **Disposal** — teardown = **destroy instance + KMS key = crypto-shred** (satisfies MP.3.8.x, §164.310(d),
  Art.17); no "purge scattered rows and prove it."
- **Evidence-export / DSAR** — a per-instance export can't leak another MSP's rows (kills RISK #23 at the
  MSP boundary).

**Trade-off:** N instances multiply the *operational* surface (patch/monitor/back-up/key-rotate/evidence
per instance) — which is why the self-monitoring + expiry registry (RISK #14) and templated provisioning
(RISK #15) must be built so N-instance ops stay auditable.

## Phased "address in source" plan

Ordered by compliance leverage per unit effort, up the [FORK-STRATEGY.md](./FORK-STRATEGY.md) ladder
(config-flag > superset new-files > core edit; RFC-to-core where upstream accepts).

**Phase A — foundational, mostly config-flag + infra (first):**
1. **Audit → WORM SIEM** (RISK #9). Flip `shouldEnableAuditLog` via config; point `server/logging/` at
   Firehose/Kinesis → S3 Object Lock / immutable SIEM. Covers AU / §164.312(b) / CC7 / Art.30-32. **S, config-flag.**
2. **Encryption at rest.** Infra: RDS/EBS + per-instance KMS CMK (now). Product: extend `EncryptAndEncode`
   to a dedicated secrets table (RISK #3). **Infra S; secrets table M–L.**
3. **MFA that counts.** Phishing-resistant MFA at the IdP + Cloudflare Access/WARP (infra/config). Harden
   non-expiring tokens. Covers IA / §164.312(d) / CC6. **S–M.**
4. **Retention & disposal config.** `ActivityExpiryWindow`, WORM as system-of-record, table partitioning;
   document crypto-shred teardown. **S–M.**

**Phase B — fork-only features (superset dirs):**
5. RBAC clean-room rebuild, deny-by-default + negative-auth suite (RISK #1/#20; rides Teams). **L–XL, core edits — minimize surface.**
6. Destructive-action guardrails (RISK #5): WebAuthn step-up + second-approver + rate-limit/breaker + break-glass. **M–L, new authz files.**
7. Tenant provisioning/teardown + crypto-shred tooling (RISK #15). **M.**
8. Self-monitoring: heartbeats + expiry registry + notification sink (RISK #14). **M, license-agnostic cron.**
9. Per-tenant KMS DEK envelope encryption for all secrets (RISK #3 full). **L.**

**RFC-to-core candidates (most rebasable):**
- Config to disable/extend activity auto-deletion + HMAC tamper-evident activity export.
- Extend `mdm_config_assets`-style encryption to `AppConfig.Integrations` secrets (closes a real upstream gap).
- HMAC-signed activities webhook + stable event id (OSS.md §8.5, S).
- The `HostStatusProvider` seam (already the designated upstream candidate).

**Stay fork-only:** per-MSP instance orchestration, per-tenant KMS DEKs, the second-approver primitive,
tenant crypto-shred teardown, the RBAC rebuild.

## Key gotchas (verified)
- **Built-in MFA is email magic-link only** (`sessions.go makeMFAEmail`) — does not meet IA.3.5.3
  phishing-resistance or the bar auditors now want. Use IdP + Access/WARP.
- **Activities are hard-DELETED** on a rolling window (`activity/internal/mysql/activity.go:200`) — MySQL is
  not your audit system of record; stream to WORM.
- **Integration secrets are plaintext** (`app_configs.go SaveAppConfig`) — the AES-GCM helper
  (`mdm.go:213`) only covers MDM assets. Extend it / new secrets table.
- **FIPS 140** (CMMC) = a Go FIPS/BoringCrypto **build variant**, not a code feature.
