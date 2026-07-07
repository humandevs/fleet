# WIP — Fleet RMM scope reconsideration (stashed 2026-07-06)

Stashed mid-thought before a reboot (HDD swap). Pick up here next session.

## The reconsideration

Question raised: does the near-term need actually justify pulling in **two whole frameworks**
(Payload + NestJS)? Maybe not. Fleet already has a capable **agent for every device type** — it seems
wasteful *not* to make Fleet's agent our RMM agent rather than building/adopting a separate one. And we can
already pull Fleet data via CLI today, so possibly **all we need is to expose that data over a local REST API
we call**, plus a thin integration/policy layer — instead of a full separate platform.

This reopens (in a good way) open item #2 from [`stack-decisions.md`](./stack-decisions.md): **the MVP
subset.** The leaner path to evaluate first:

> **Fleet (stock: device engine + agent + software deploy) + a thin integration/policy layer + Ansible for
> install/keep-installed** — and defer the full Payload+NestJS platform until the need is proven.

## What we actually need near-term (scoped)

1. **Device data** — Fleet already provides.
2. **WMI data (Windows)** — osquery custom extension (via TUF), no fork.
3. **Push / install + keep-installed + monitor** these external agents:
   - ScreenConnect
   - Action1
   - MeshCentral
   - Bitdefender GravityZone SDK
   Install + keep-installed possibly via **Ansible** (+ Fleet software deploy); monitor presence/health and
   reinstall if missing.
4. **Client > Site >** hierarchy (tenancy) with inheritance.
5. **Per-client/site policies** for Bitdefender + Action1 — e.g. *when to install updates*, *when to prompt
   for reboot*.
6. **Reboot prompt with a delay / allow-timer**, settable **per client/site**, **default = inherit parent**.
   (Called out as genuinely useful.)

## Deferred (we barely use it)

- Scripting engine and policy engine — only occasional ad-hoc PowerShell / batch / VBScript. Not a near-term
  driver.

## Open tensions to resolve next session

- **"Extend Fleet" vs "thin sidecar next to Fleet."** We decided *not to fork Fleet* (stock, headless). So
  "extend Fleet to interface with external systems and surface data" most cleanly = a **thin sidecar service**
  that calls Fleet's REST API + adds the integration/policy/tenancy layer + a local REST API — NOT modifying
  Fleet's Go. Confirm this reading.
- **"Surface data in the app" — which app?** If it means Fleet's *own* UI, that implies a frontend fork
  (which we're avoiding). If it means our own thin UI, no fork. Decide.
- **Does the leaner path replace the big platform for MVP,** or just precede it? Likely: build the thin
  sidecar first; graduate to Payload/NestJS only when tenancy/automation/UI outgrow it.

## Immediate next step (next session)

Decide leaner-sidecar vs full-platform for MVP, then draft the **cross-system device identity model**
(canonical device ↔ Fleet host ↔ OTel ↔ Mesh ↔ Action1 ↔ Bitdefender) — still the backbone either way.
