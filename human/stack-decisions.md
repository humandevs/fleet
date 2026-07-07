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
