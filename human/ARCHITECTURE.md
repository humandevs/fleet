# TacticalRMM vs Fleet — Architecture & Consolidation Analysis

**Purpose:** decide whether to standardize/consolidate the Human-ISM UEM initiative on **Fleet** or
**TacticalRMM**, and, if consolidating, in which direction. Grounded in a full code review of both trees
(`F:\Users\Nate\GitHub\tacticalrmm`, `develop`/`human-dev` @ `2195c3b3`, and this Fleet fork).

> **Scope note:** TacticalRMM's develop branch was pulled to `../tacticalrmm`. This doc reviews the **server**
> repo (Django API + Go `natsapi` + Docker/Ansible). The **agent** (`github.com/amidaware/rmmagent`) and the
> **web frontend** (`github.com/amidaware/tacticalrmm-web`) are **separate repos** and were not present to read.

---

## TL;DR — recommendation

**Build on Fleet's MIT core. Treat TacticalRMM strictly as a design reference — do not fork its code.**

Three findings force this, in priority order:

1. **Licensing (decisive).** TacticalRMM's core license is *explicitly not open source*. It forbids offering
   the functionality "as part of a SaaS service or product... whether or not branded as Tactical RMM,"
   requires AmidaWare's **prior written approval** for any commercial use of a modified/derivative version,
   and has a **copyleft "publish your complete source publicly"** hook. The agent is a separate non-OSS repo
   (signed + cross-platform builds are sponsorship-paywalled). **You cannot lawfully fork, rebrand, and sell
   it.** Fleet's core is **MIT** — fork, rebrand, embed in SaaS, sell, keep changes private. *(Details §5.)*
2. **Agent swap is a rewrite, not a swap.** Tactical's server is welded to an **undocumented, unversioned,
   imperative msgpack-over-NATS command protocol** keyed on `agent_id`, plus a *second* MeshCentral agent for
   remote control. Its ~40 commands are Windows-imperative (registry edit, service control, PTY, Windows
   Update) — the opposite paradigm to osquery/orbit's declarative pull. You cannot drop Fleet's agent into
   Tactical's server, nor feed orbit data into it, without gutting one side. *(Details §3.1.)*
3. **Operational weight.** Tactical is ~12 containers, 3 datastores (Postgres + MongoDB + Redis), 2 transports
   (NATS + Redis), 3 language runtimes (Python/Go/Node). Fleet is **one Go binary + MySQL + Redis.** *(§4.)*

**What Tactical does better and should inform the Fleet roadmap:** two-tier MSP tenancy (Client→Site),
four-tier policy inheritance, threshold-based checks with severity, calendar task scheduling, native
email/SMS/severity-routed alerting, and an integrated remote-desktop/terminal/file story (via MeshCentral).
These are the **feature gaps to close on Fleet** — see the backlog in §6.

---

## 1. TacticalRMM at a glance

RMM (remote monitoring & management), by AmidaWare LLC. Django + Vue, a Go agent, MeshCentral for remote
access. Feature set: TeamViewer-like remote desktop, real-time shell, remote file browser, registry editor,
multi-shell script execution (batch/ps/python/nushell/deno), Windows patch management, threshold checks with
email/SMS/webhook alerting, scheduled task runner, Chocolatey software install, hardware/software inventory.
Windows-first (Win7→Server 2025); Linux/macOS agents exist but are sponsorship-gated.

### Component map

| Layer | TacticalRMM | Where |
|---|---|---|
| Web UI | Vue 3 + Quasar SPA, **pre-built tarball** served by static nginx; runtime API host via `window._env_` | separate repo `tacticalrmm-web` (pinned `WEB_VERSION=0.101.64`) |
| REST API | Django 4.2 LTS + DRF 3.15, Knox token auth, served by **uWSGI** | `api/tacticalrmm/` |
| Async | **Celery** worker (`--autoscale=20,2`) + **celery beat** scheduler, Redis broker | `*/tasks.py`, `tacticalrmm/celery.py` |
| Websockets | Django **Channels** over **uvicorn** (dash live updates, cmd/terminal streaming) | `asgi.py`, `*/consumers.py` |
| Agent transport | **NATS** (msgpack RPC) + a Go **`nats-api`** bridge that writes agent check-ins straight to Postgres | `natsapi/`, `main.go` |
| Remote access | **MeshCentral** (Node) + **MongoDB** — bundled, co-proxied, linked per host by `mesh_node_id` | `core/mesh_utils.py`, `core/utils.py` |
| Agent | Go `rmmagent`, Windows-centric, **separate non-OSS repo** | `github.com/amidaware/rmmagent` |

### Django apps (`api/tacticalrmm/tacticalrmm/`)

`agents` (Agent model + NATS RPC), `checks` (7 check types), `autotasks` (scheduled scripts), `alerts`
(templates/channels), `automation` (policies + inheritance), `clients` (Client→Site tenancy), `accounts`
(users/roles/API keys/2FA), `core` (settings/custom fields/keystore/URL actions/Mesh creds), `scripts`
(multi-shell library), `software` (choco + inventory), `winupdate` (patch mgmt), `services` (Win services),
`logs` (audit/pending-actions/debug), `apiv3`+`apiv4` (agent-facing REST), `ee` (reporting + SSO, token-gated).

---

## 2. Runtime topology & operational weight

```
                        Internet (:80/:443)
                              │
                       tactical-nginx  (reverse proxy)
         ┌──────────────┬──────────────┬────────────────┐
         ▼              ▼              ▼                ▼
  tactical-frontend  tactical-backend  tactical-websockets  tactical-meshcentral
   (static SPA)       (Django/uWSGI)    (Channels/uvicorn)      (Node)
                         │   │             │   │                   │
                         ▼   ▼             ▼   ▼                   ▼
                  tactical-postgres   tactical-redis        tactical-mongodb
                         ▲   ▲        (Celery broker +            (Mesh only)
                         │   │         Channels layer)
              ┌──────────┴┐  └───────────┐
              ▼           ▼               ▼
        tactical-celery  celerybeat   tactical-nats
        (worker)         (scheduler)  ├─ nats-server (:4222) ◄── agents
                                      └─ nats-api (Go) ──► postgres
        tactical-init (one-shot bootstrap; all wait on tactical.ready)
```

**~12 containers · 3 datastores · 2 transports · 3 runtimes.** Six of those containers are the *same* image
run with different commands (backend/websockets/celery/celerybeat/init). MongoDB exists **only** because
MeshCentral requires it. Several pinned deps are near/past EOL (Postgres 13, Mongo 4.4, Redis 6).

**Fleet:** one static Go binary (frontend embedded via go-bindata) + MySQL + Redis. Scale = run more copies
behind a load balancer. Back up = MySQL. Upgrade = one versioned binary that runs its own migrations.

For a small IT/MSP shop the maintenance tax is the story: Tactical means patching/monitoring Django, Celery,
uWSGI, uvicorn, a Go NATS bridge, MeshCentral, Postgres, Mongo, Redis, and NATS, with coordinated two-database
backups (hence the repo's dedicated `backup.sh`/`restore.sh`). Fleet is dramatically lighter — the tradeoff
being it ships no remote-desktop engine and folds async work into its own binary instead of a Celery cluster.

---

## 3. How the agent actually works (and why it can't be swapped)

### 3.1 Transport & protocol

Three channels, not one:

- **NATS (msgpack RPC)** — server→agent commands via `Agent.nats_cmd()` (`agents/models.py:916`). Subject =
  the agent's `agent_id`; payload = `{"func": "...", "payload": {...}}`. `wait=True` → synchronous
  request/reply (default 30s); `wait=False` → fire-and-forget, agent POSTs results back later. ~40 `func`s:
  `rawcmd`, `runscript`, `winservices`/`winsvcaction`, `eventlog`, `registry_*`, `schedtask`, `installchoco`,
  `getwinupdates`/`installwinupdates`, `procs`/`killproc`, `rebootnow`, `terminal_*` (PTY), `agentupdate`, etc.
- **HTTPS REST (`apiv3`/`apiv4`)** — agent-pull (check/task definitions) + agent-push (results, software,
  Windows-update state, registration). Agent authenticates as its own Django user via a DRF token.
- **The Go `nats-api`** (`natsapi/svc.go`) — a thin telemetry sink: subscribes to `"*"`, dispatches on
  `msg.Reply` (`agent-hello`, `agent-disks`, `agent-winsvc`, `agent-wmi`, …), and writes straight to Postgres,
  **bypassing Django** for high-frequency check-ins. Only ~300 lines; not the command dispatcher.

**The coupling core** is `reload_nats()` (`tacticalrmm/utils.py:178`): Django *generates the NATS server
config* and hot-reloads it on every agent registration, with per-agent ACLs where **`agent_id` is
simultaneously the DB PK, NATS subject, NATS username, and Django auth user**, and the agent's NATS password
*is* its REST token. The protocol has no schema/IDL/version — it lives only in matched Python call sites and
the closed-source Go agent.

### 3.2 Why "roll Fleet's osquery agent into Tactical" (or vice-versa) fails

- **Opposite paradigms.** Tactical = imperative, bidirectional, always-connected command-and-control. Fleet's
  orbit/osquery = declarative pull (poll for config/queries, report results). osquery has no concept of "start
  a PTY," "edit this registry value," "run this PowerShell and stream stdout," or "reboot now."
- **Two agents required.** Full function needs the Tactical NATS agent **and** a paired MeshCentral agent
  (`mesh_node_id`). No osquery-family agent can provide remote desktop/files at all.
- **Undocumented contract.** Replacing the agent means reverse-engineering `rmmagent` and reimplementing ~40
  Windows-imperative commands — a full rewrite of orbit's execution engine against a moving target.

**Verdict:** feature migration into Fleet (below) is the only realistic technical path; agent substitution is
not.

---

## 4. Feature-by-feature vs Fleet

### Monitoring — checks / tasks / alerts

- **Checks** (`checks/models.py:31`, types at `constants.py:202`): 7 built-in types — diskspace, ping,
  cpuload, memory, winsvc, script, eventlog — with **warning/error thresholds + severity**, `fails_b4_alert`
  consecutive-failure debounce, per-check intervals, and rolling-average smoothing for cpu/mem. Pass/fail is
  evaluated **server-side in Python** from raw metrics the agent reports. Append-only `CheckHistory` gives
  graphable time-series.
  - *Fleet analog:* **policies** (boolean osquery SQL → pass/fail) + scheduled **queries**. **Gaps:** no
    warning tier, no threshold primitives (CPU%/mem%/disk-free), no consecutive-failure debounce, no
    return-code→severity mapping. Fleet's strength: osquery SQL over hundreds of tables, genuinely uniform
    cross-platform, far better for inventory/vuln/ad-hoc questions.
- **Automated tasks** (`autotasks/models.py:59`): first-class calendar scheduler (daily/weekly/monthly/
  monthly-DOW **bitmasks**), multi-step action chains, triggers incl. **on-check-failure**, run-once,
  onboarding. Windows tasks are delegated to the **native Windows Task Scheduler**; POSIX runs server-side on
  celery beat.
  - *Fleet analog:* scripts + scheduled queries. **Gap:** no cron-like scheduled-arbitrary-script runner with
    repetition/expiry/on-failure triggers.
- **Alerts** (`alerts/models.py:32`): `AlertTemplate` with **native email (SMTP) + SMS (Twilio)** channels,
  per-channel severity allow-lists, dedup, snooze, maintenance-mode suppression, resolve/recovery
  notifications, periodic re-alerting, run-a-script-on-alert (agent **or** server), and site/client/agent
  exclusion scoping.
  - *Fleet analog:* automations (policy-failure webhooks, Jira/Zendesk, vuln automations). **Gaps:** no native
    SMS, no template-level per-channel severity routing, no debounce, no built-in re-alert cadence. Fleet's
    strength: deeper ticketing integrations and vuln-specific automations.

### Remote access — the biggest gap

Tactical implements **none** of remote desktop / interactive terminal / file browser itself — it delegates
100% to a **bundled MeshCentral** (`agents/views.py:428`), minting signed deep-links into Mesh's own web
panels (`viewmode=11`=desktop, `12`=terminal, `13`=files), linking devices by `mesh_node_id`, and mirroring
Tactical users/permissions into Mesh over a websocket admin API (`core/mesh_utils.py`). Mesh is also the
out-of-band recovery channel when an agent's NATS is dead (`Agent.recover`).

- *Fleet:* **nothing comparable.** This triad is Tactical's single hardest-to-replicate asset, and it only
  exists because Tactical co-installs MeshCentral + MongoDB. Replicating on Fleet = embed/bridge MeshCentral
  (new service + Mongo + user/perm mirroring + proxy + host↔node linking) **or** build a p2p desktop/PTY
  channel into fleetd from scratch.

### Scripts / software / patching / services

| Capability | TacticalRMM | Fleet today | Consolidation note |
|---|---|---|---|
| Multi-shell scripts | 6 shells (ps/cmd/python/shell/nushell/deno), args, env vars, `{{token}}` injection, `{{snippet}}` includes, community library, run-as-user, run-on-any-online | Scripts by shell (sh/ps1/py/bat), saved-script library | **Bridgeable** — closest analog. Add nushell/deno, snippet includes, arg/env templating, run-as-user. |
| Chocolatey install | on-demand choco bootstrap + `installwithchoco` + pending-action callback | Own software-install pipeline (FMA/VPP/MSI/pkg), **no choco** | Different model; map to Fleet installers or add choco (net-new). |
| Windows Update mgmt | full: per-KB inventory, severity auto-approval **policies with client/site inheritance**, scheduling windows, reboot policy, failure reprocessing | Defers Windows OS updates to **MDM CSPs**; no per-KB agent WUA approval workflow | **Significant gap** for non-MDM/agent-driven patching. Net-new WUA scan/approve/schedule model. |
| Services control | live start/stop/restart/edit-start-type via agent | none first-class (ad-hoc via script) | Minor — implement as scripts or a thin fleetd service API. |
| Registry editor, PTY, procs/kill, reboot | yes (NATS `func`s) | none | Net-new agent capabilities if wanted. |

### Multi-tenancy, RBAC, automation policy

- **Tenancy** (`clients/models.py`): a genuine **two-tier `Client → Site → Agent`** MSP tree, with
  workstation/server policy + alert-template FKs at *both* levels plus a global default.
  - *Fleet:* single-level **Teams/Fleets** (`team_id`). Tactical's two-tier model is purpose-built for MSPs
    managing many customer orgs — a better tenancy model to emulate.
- **RBAC** (`accounts/models.py:106`): single `Role` per user = a flat bag of ~80 boolean action perms, with
  per-tenant scoping bolted on via two M2Ms (`can_view_clients`/`can_view_sites`). **Limitation:** scoping is
  *view-only* — you cannot express "admin on Client A, read-only on Client B" without separate roles. API keys
  inherit their owner's role. 2FA (TOTP) built-in; SSO in `ee/` (token-gated).
- **Automation policy** (`automation/models.py`): 4-tier inheritance resolved in `Agent.get_agent_policies()`
  (`agents/models.py:585`) — Agent-direct → Site → Client → Global default, split by monitoring_type
  (server/workstation), with `block_policy_inheritance` opt-out at each level, exclusion lists, and
  `enforced`-policy override semantics. **More sophisticated than Fleet's policy model** and worth emulating.

---

## 5. Licensing — the decisive constraint

Two layers, **neither is open source**:

- **Core server — "Tactical RMM License v1.0"** (`LICENSE.md`, © AmidaWare LLC):
  - `LICENSE.md:15` — *"is not an open-source software license."*
  - Permitted: run it (even modified) to monitor/manage **your own** and **your customers'** networks — an
    internal-tooling grant.
  - **Forbidden** (`:22-27`): the functionality (whole/partial/modified/**derivative**) *may not be made
    available "as part of any other commercial or for-profit service," including a SaaS product, managed
    hosting, paid install/config, or "the offer for sale, distribution or sale of any service or product
    (**whether or not branded as Tactical RMM**)."* Rebranding does **not** cure this.
  - `:29` — **any** commercial/for-profit use of a modified or derivative version requires **prior written
    approval**.
  - `:35-36` — **copyleft**: any derivative made available to anyone must ship complete source **publicly**.
  - `:62-63` — trademark lock (no use of the "Tactical RMM" marks).
- **`ee/` subtree — "EE License"** (`ee/LICENSE.md`, © Amidaware Inc.): requires a **sponsorship token** to
  use at all; `:17` forbids copy/distribute/sublicense/sell of the EE code or derivatives; `:19` forbids
  removing/bypassing the license-key checks. Reporting, SSO, and **white-labeling** live here — i.e.
  white-labeling is a paid feature you're contractually barred from unlocking.
- **Agent** (`rmmagent`, separate repo): same non-OSS license; **signed Windows + macOS/Linux builds are
  sponsorship-only**. The community Windows agent is unsigned.

**Verdict:** TacticalRMM is legally **unusable as a base for a rebranded/resold/hosted product** without a
negotiated commercial agreement from AmidaWare. **Fleet's MIT core is the only one of the two you can lawfully
fork, rebrand, and commercialize** (attribution being the sole obligation; Fleet's own paid features are
walled in `ee/` but the core is clean). This aligns with the existing Human-ISM UEM-fork decision.

---

## 6. The three paths — and the recommended one

| Path | Description | Verdict |
|---|---|---|
| **A. Standardize on Fleet, port Tactical's ideas** | Keep Fleet's MIT agent + lean stack; reimplement Tactical's best features natively (or bridge MeshCentral for remote access) | ✅ **Recommended** — only lawful, technically coherent option |
| **B. Standardize on TacticalRMM** | Adopt/fork Tactical as the platform | ❌ Ruled out by licensing (§5) — cannot sell/host/rebrand; copyleft; paywalled agent |
| **C. Roll Fleet's osquery agent as Tactical's agent** | Swap orbit in for `rmmagent` | ❌ Ruled out technically (§3.2) — opposite paradigm, undocumented protocol, still needs MeshCentral = full rewrite |

### Recommended roadmap — features to build on Fleet (prioritized by gap size × value)

1. **Remote access (largest gap).** Decide: **bridge MeshCentral** (fastest to parity — mirror Fleet's
   remote-desktop/terminal/file story via a Mesh sidecar linked by a `mesh_node_id`-equivalent on the host)
   **vs** build a native fleetd PTY/desktop channel (leaner stack, much larger build). MeshCentral is itself
   liberally licensed (Apache-2.0) — bridging it is legally clean.
2. **Threshold checks + severity.** Extend Fleet policies with warning/error tiers, CPU/mem/disk-free
   primitives, consecutive-failure debounce, and return-code→severity mapping. (Aligns with the existing
   community coverage-matrix work.)
3. **Native alerting channels.** Add email (SMTP) + SMS (Twilio) with per-channel severity routing, dedup,
   re-alert cadence, and resolve notifications — on top of Fleet's existing webhook/integration automations.
4. **Scheduled task runner.** A calendar/cron scheduler for saved scripts, with on-policy-failure triggers,
   repetition, and expiry.
5. **Two-tier tenancy + layered policy inheritance.** Emulate Client→Site (above Fleet Teams) and the
   Agent→Site→Client→Global policy resolution, with per-level opt-out and enforced overrides. Highest-value
   *architectural* borrow for MSP use.
6. **Agent-driven Windows Update approval/scheduling** for non-MDM Windows patching (per-KB inventory,
   severity auto-approval, maintenance windows).
7. **Script parity:** nushell/deno shells, `{{snippet}}` includes, `{{token}}` arg/env templating,
   run-as-user, run-on-any-online.

Everything in 2–7 is native Fleet extension work (Go + osquery/orbit + frontend) with **no MeshCentral/Mongo
tax**; item 1 is the one place a sidecar may be justified.

---

## 7. Key file references (TacticalRMM)

- Agent + NATS RPC: `agents/models.py:916` (`nats_cmd`), `tacticalrmm/utils.py:178` (`reload_nats`),
  `natsapi/svc.go`, `agents/utils.py:103`, `apiv3/views.py` (agent REST), `agents/consumers.py` (PTY bridge).
- Monitoring: `checks/models.py:31`, `constants.py:202`, `checks/models.py:363` (`handle_check`),
  `autotasks/models.py:59`, `alerts/models.py:32`,`:289` (`handle_alert_failure`).
- Remote access: `core/mesh_utils.py:89`, `core/utils.py:98`/`:182`, `agents/views.py:428`.
- Tenancy/RBAC/automation: `clients/models.py:18`,`:93`, `accounts/models.py:106`, `tacticalrmm/models.py:11`
  (`PermissionQuerySet`), `automation/models.py:21`, `agents/models.py:585` (`get_agent_policies`).
- Scripts/software/patch: `scripts/models.py:15`, `software/views.py:47`, `winupdate/models.py:89`,
  `services/views.py`.
- Deploy/stack: `docker/docker-compose.yml`, `install.sh`, `natsapi/`, `main.go`, `requirements.txt`.
- Licensing: `LICENSE.md`, `ee/LICENSE.md`, `README.md:46-51` (sponsorship features).
