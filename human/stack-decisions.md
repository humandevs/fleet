# Stack decisions — living record

The options matrix is [`stack-options-rmm.html`](./stack-options-rmm.html) (17 layers, all MIT/Apache/free-SaaS).
This file is **what we actually chose and why**, as decisions firm up. See also
[`ARCHITECTURE.md`](./ARCHITECTURE.md) (Fleet vs TacticalRMM) for the "why Fleet, not Tactical" reasoning.

_Last updated: 2026-07-06._

## Backend framework layout — Payload + NestJS (Feathers dropped, Hono deferred)

Two backend frameworks, each with a distinct reason to exist:

| Framework | Owns | Notes |
|---|---|---|
| **Payload CMS** (on Next.js) | Management plane / system-of-record: users, workspaces, device registry, billing, alert rules; internal admin UI; lifecycle hooks; Tier-1 jobs; web-facing route handlers | Code-first, Drizzle-native. Payload mounts inside Next.js — one deployable serves web UI + admin + API. **Also the single IdP** (see below). |
| **NestJS** | RMM device plane: telemetry/MQTT ingestion, BullMQ (Tier-2 jobs), provider-plugin modules, real-time **gateways**, command dispatch, **and the runtime metadata engine** (Drizzle + pgroll + PostGraphile) | DI/module system fits the provider-plugin architecture. The metadata engine is DB-bound (long-lived Postgres connections, schema DDL) so it belongs on Node, not the edge. |

- **Feathers is dropped.** Its auto REST+WebSocket CRUD role is covered by NestJS gateways.
- **Hono is deferred, not adopted.** Next's route handlers (Edge runtime where needed) + Payload cover the web/API surface, and Hono's assigned job (the metadata engine) is DB-bound → better on Nest/Node. **Reach for Hono only if** we need standalone, stateless, globally-distributed **edge** endpoints where shipping Next's runtime is too heavy — e.g. high-volume agent-ingest / webhook fan-in, or edge-cached metadata reads with sub-5ms cold starts (most compelling on a Cloudflare-centric hosting model). Not a v1 dependency.
- **Boundary rule:** Payload owns entities + identity + back-office; NestJS owns the high-throughput device plane + metadata engine. They share Postgres/Drizzle but **each owns its own tables** and integrate via **API + Payload-issued tokens**, never by reaching into each other's tables.

## Auth — one IdP

- **Payload is the single identity provider.** Users/orgs/RBAC live in Payload; NestJS (and Fleet/Mesh via SSO) **validate Payload-issued tokens** — no second auth system. This is the guardrail that keeps a two-framework backend clean.
- Fine for v1. If per-tenant SSO/OIDC/SAML federation grows heavy, front it with a dedicated IdP (Keycloak/Better-Auth) that **both** Payload and Nest trust — still exactly one IdP.

## Fleet — stock, headless, NOT forked

- Fleet is an **unmodified upstream dependency**, run as the official Docker image and driven via REST API + webhooks. The `human/appliance/fleet-stack/` compose **is the production pattern**, not a stopgap.
- This **retires the fork-image / appliance-build workstream** (the tar/git/HOME/GOPATH/`orbit/pkg/build` saga — all obviated by never rebuilding Fleet).
- The `server/community/*` coverage-matrix + integration-provider work **re-homes** into NestJS/Payload provider modules (cross-provider correlation is a management-plane job). Custom Windows data → **osquery custom extensions via TUF** (not a fork).

## Endpoint agent — one branded overseer

- **One branded installer/overseer** (our code) lays down and supervises the endpoint stack; a tiny watchdog keeps the main agent alive / reinstalls on failure (TacticalRMM-style agent+watchdog).
- It **leans on Fleet's software-deployment + orbit/TUF** as the delivery/update mechanism for the other agents (OTel Collector, Mesh agent, Action1 agent) — no bespoke update transport.
- Net effect: **one install, one thing to monitor**, even though ~4 processes run underneath. Resolves the agent-sprawl concern.

## Unchanged from the options matrix

- **ORM/DB:** Drizzle + Postgres + **TimescaleDB** (RMM metrics). Migrations: Drizzle + pgroll.
- **Data-shape split:** Fleet+osquery = inventory/posture/compliance/software/CVE/scripts. OTel Collector = continuous metrics (CPU/mem/disk/net/WMI/eventlog). Never overlap.
- **Patch:** Action1. **Remote access:** MeshCentral (vendored, deep-link device→session; ScreenConnect premium per-client; don't extend Mesh).
- **Mobile:** RNR + Unistyles v3. **Web admin:** Refine + shadcn/ui. **Sync:** PowerSync + Electric.
- **Transport:** Mosquitto (agents→broker) + NATS (internal bus). **Queues:** Payload Jobs / BullMQ / Temporal (deferred).
- **Runtime schema engine:** custom **NestJS module** + Drizzle + pgroll + PostGraphile (Twenty-CRM-style). GraphQL from PostGraphile, not Nest code-first. (Was Hono in the options matrix; re-homed to Nest since it's DB-bound.)

## Open / load-bearing (design before building)

1. **Cross-system device identity** — one canonical device record keyed on stable hardware id (serial + hardware UUID), with a mapping table to Fleet host UUID ↔ OTel `host.id` ↔ Mesh node id ↔ Action1 endpoint id. The backbone; design first.
2. **MVP subset** — the v1 minimal stack (likely Payload + Drizzle/Postgres + NestJS + Fleet + Action1 + Mesh), deferring Hono/metadata-engine/n8n/Temporal until data shapes demand them.
3. **Fleet ↔ platform integration contract** — which Fleet API/webhook surfaces the `DeviceEngineProvider` consumes; tenancy (workspace/client → Fleet Teams); token federation.

## OPSI — evaluated and REJECTED as base/complement (2026-07-28)

Code-level evaluation of the locally cloned repos (opsiconfd, opsiclientd, opsi-webgui, opsi-docker,
opsi-quick-install, uib's `lazarus` monorepo containing opsi-script). Verdict: **Fleet's MIT core stays
the device engine; OPSI's only role for us is clean-room design reference.**

- **License:** everything is **AGPL-3.0-only** (network copyleft — SaaS does NOT escape, §13), and
  `opsiclientd/nonfree/` (WAN cache) is all-rights-reserved with **no license grant**, imported by the
  AGPL agent — the shipped client is a mixed artifact only uib can redistribute. Appliance distribution
  of (A)GPL code triggers full corresponding-source obligations; in-process addons (their only extension
  seam) are derivative works. Fleet's MIT core has none of this — that asymmetry is why Fleet is the base.
- **Paywall targets exactly our sellable surface**, enforced by uib-RSA-signed license files checked in
  AGPL code: single worker without `scalability1` (opsiconfd/manager.py:85-99), client remote-exec gated
  on `vpn` (messagebus/websocket.py:102,113), **macOS/Linux agents paid** (macOS agent source not even
  public), `userroles` paid. Correction to our earlier read: **software/hardware audit (swaudit) is FREE**;
  so is the full software-distribution stack + PXE netboot + Windows agent + webgui + MySQL backend.
- **"GPL but paid" is coherent dual-licensing:** uib owns 100% of copyright, so it sells signed unlocks.
  Their **co-funding→free** model is real (freed modules carry `client_number 999999999` in
  tests/backend/rpc/test_general.py:379-401). The MODEL (sole copyright + own license gate + co-funding)
  is worth imitating; the code is not.
- **Build/ops reality:** core deps (python-opsi/-legacy, opsicommon, configed, bootimage) come only from
  uib's **private PyPI**; official docker apt-installs prebuilt debs; opsi-script needs Lazarus/FreePascal
  on three OS-specific runners. Python 3.14 + Pascal + Java + Redis + MariaDB + Samba + TFTP — wrong stack
  for a Go/TS team.
- **Patterns to mine (clean-room: from the evaluation notes ONLY — implementing agents never read the
  OPSI clones):** productOnClient action-request/installationStatus state machine + dependency resolution
  + per-depot version pinning (desired-state model for our Windows install layer over Fleet's scripts
  engine); opsi-script's section-based install DSL (declarative sections + imperative escape hatches +
  server RPC callback incl. license-key pools) as blueprint for our declarative install manifests;
  event-driven agent lifecycle; free PXE→bootimage→unattended-install flow as prior art for the tabled
  WIM imaging vision (note: wim-capture and local_imaging are PAID even in OPSI).

**Premium-price concern resolved:** Fleet's $7/host/mo never hits our cost structure — we run Fleet Free;
premium-gated needs were rebuilt in `server/community/` (coverage matrix/providers/dashboard) or routed
around (scripts engine, tenancy in the platform). Our marginal Fleet cost per host is $0.

**Android lane (2026-07-28): TinyMDM** (~$2.20/device/mo) confirmed API-fit for the provider pattern:
MSP manager-key + `X-Account-Id` per tenant, `GET /managers/manageable_accounts`, `GET /devices` maps by
serial/IMEI with `last_sync_timestamp` staleness + `policy_id` presence as the coverage cell; batch
lock/wipe/reboot/kiosk; policy-scoped silent app push; QR enrollment. Gaps accepted: no policy CRUD via
API (console-side templating is a manual onboarding step), static master key (vault, break-glass
handling), poll-only. **Escape hatch: Google Android Management API — which is FREE with full policy
CRUD** (TinyMDM is itself an AMAPI console; "Google is pricier" was wrong). Intune rejected (~$8 +
GDAP/Lighthouse plumbing). ManageEngine rejected (AV/EDR lock-in vs our Huntress+Bitdefender stack).

**Upstream contribution posture (docs-verified):** ADR-0002 only rejects the GitHub-Discussions TOOL for
internal chats — but the drafted-and-closed gate for product-changing community PRs lives independently
in handbook/engineering/README.md:137-146 (+ product-groups.md:1150-1154), so a working-code "RFC" mega-PR
has a documented zero-cost close path and exerts no pressure. No extension ADRs exist (0001-0010 checked);
no CLA. The modular-monolith doc is Go-server-only and defines contexts by BUSINESS DOMAIN — an
"Integrations" context fits its Activity reference pattern; "community" (a provenance label) does not,
and frontend/community has no grounding in it. Play: (a) small non-product PRs (merge directly per
handbook:131); (b) feature-request issues per capability (their documented intake); (c) optionally ONE
docs-only ADR PR proposing an "Integrations" bounded context, status Proposed — the only PR-shaped
proposal vehicle their docs invite; (d) offer our working code only AFTER an issue is prioritized into
drafting; (e) never touch ee/ (its license assigns Fleet ownership of modifications). Fork-side
server/community/ + frontend/community seam remains the strategic home regardless.
