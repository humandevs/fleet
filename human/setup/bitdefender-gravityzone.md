# Bitdefender GravityZone setup

Ingests **GravityZone AV/EDR** endpoint inventory + threat/incident status for the Endpoint Protection
dashboard; optionally pushes scan/remediation actions. Design:
[../OSS.md §8.2](../OSS.md#82-avedr-dashboards-bitdefender-gravityzone--huntress).

> **Auth model, transport, and rate limit: confirmed** against Bitdefender's official GravityZone
> Public API docs. Exact response **field names** live on JS-rendered method pages — confirm those
> against a live account or the downloadable PDF API guide.

## SDK vs. API — use the Control Center API, not the old SDK
Bitdefender's older **direct-integration SDK is deprecated / being sunset** in favor of GravityZone
integration. There is **no first-party Go/Python/JS SDK**; what's marketed as the "GravityZone SDK" is
the **Control Center Public API** (JSON-RPC 2.0 over HTTPS) plus PDF guides/snippets. **Hand-roll a thin
JSON-RPC client** (`fleethttp.NewClient()`) — it's a handful of POSTs with Basic auth. (The separate
"RMM SDK Tools" / MSP SDKs are a different thing and not what we need.)

## Auth model — HTTP Basic with the API key as username
```
Authorization: Basic base64(APIKEY + ":")      # username = API key, password = EMPTY
Content-Type: application/json
body: {"jsonrpc":"2.0","method":"…","params":{…},"id":1}   # JSON-RPC 2.0, always POST
```
The static API key **is** the credential — no OAuth, no token, no refresh. Store it encrypted;
HTTPS-only. Keys are **scoped at creation** to the specific APIs they may call.

## Base URL — pick ONE convention (don't double `/api`)
Full endpoint form: **`<host>/api/v1.0/jsonrpc/<service>`**.

| Tenant | `<host>` |
|---|---|
| Cloud, EU | `https://cloudgz.gravityzone.bitdefender.com` |
| Cloud, US / non-EU (default) | `https://cloud.gravityzone.bitdefender.com` |
| On-prem | `https://<your-control-center>` |

So a Network call is `POST https://cloud.gravityzone.bitdefender.com/api/v1.0/jsonrpc/network`. Make the
host operator-configurable (other regional data centers exist; confirm yours from the console URL — a
wrong region silently 401s). **Store `base` as the host only** and append `/api/v1.0/jsonrpc/<service>`
in code, so you never end up with `…/api/api/…`.

## Get the API key
Control Center (cloud `https://gravityzone.bitdefender.com` or your regional/on-prem console) → sign in
as admin → **user icon (top-right) → My Account → "Control Center API" section → API keys table → Add**
→ enter a description, **select the APIs** this key may access (Network, Incidents, Event Push Service) →
save → **copy the key once** (cannot be re-displayed; recreate if lost).

## Key methods
| Purpose | Service · method |
|---|---|
| List managed endpoints (paged) | `network` · `getEndpointsList` — `perPage` max **100**, default 30, `page` from 1; returns `{page,pagesCount,perPage,total,items}` |
| Endpoint detail (agent/engine version, last scan, protection state) | `network` · `getManagedEndpointDetails` (`endpointId`) |
| Threats / EDR incidents | `incidents` · `getIncidentsList` / `getIncident` / `getIncidentsByIds` — **require v1.1 or v1.2** paths (not v1.0) |
| On-demand YARA scan (push action) | `incidents` · `startYaraScan` |
| Real-time detection push | `push` · `setPushEventSettings` / `getPushEventSettings` / `sendTestPushEvent` |

## Ingestion mode: poll and/or push
- **Poll** (inventory): cron pages `getEndpointsList` → `getManagedEndpointDetails` per host.
- **Push** (recommended for detections): call `setPushEventSettings` once to register **our public HTTPS
  receiver URL** + `serviceType` **`jsonRPC`** (exact casing; `cef`/`splunk` also supported) +
  `subscribeToEventTypes` (`av`, `aph`, `avc`, `fw`, `dp`, `modules`, `registration`, `task-status`,
  `new-incident`, `exchange-malware`, …). `jsonRPC` delivery sends a **configurable Authorization header
  to our receiver**, so we can validate a shared secret on each inbound POST.

## Product wiring
- Client: `server/service/externalsvc/bitdefender.go` (mirror `jira.go`): `BitdefenderClient` +
  `{BaseURL(host), APIKey}`, JSON-RPC POST with Basic auth, `doWithRetry` honoring 429.
- Config on `AppConfig.Integrations` (`Bitdefender []*BitdefenderIntegration{URL, APIKey}`, `APIKey`
  masked like `JiraIntegration.APIToken`) + `ValidateBitdefenderIntegrations` + `makeTestBitdefenderRequest`
  (a cheap `getEndpointsList` perPage=1). **Settings → Integrations → Bitdefender.**
- Poller cron in a **license-agnostic** group; **or** a push-receiver route in `handler.go`
  (`POST /api/_version_/fleet/integrations/bitdefender/events`, secret-validated).
- **Host mapping:** by **FQDN/hostname first**, fallback serial/UUID or MAC; store the GravityZone
  endpoint `id` on the Fleet host for stable re-linking.
- Host-detail "Endpoint protection" card + dashboard card (at-risk / outdated-agent / recent detections).

## Rate limits & gotchas
- **10 requests/second per API key**; 429 includes a **`Retry-After`** header — honor it.
- **JSON-RPC returns HTTP 200 even on errors** — inspect the body's `error` field, not just the status.
- Incidents methods need the **versioned path** (v1.1/v1.2); hardcoding v1.0 fails for those.
- Push receiver must be **public HTTPS, TLS 1.2+**, valid cert; validate the shared secret on every POST.
- Cloud vs on-prem method/version availability differs; the Incidents/EDR API needs the right license tier.

## Verify against a live account
- Exact response field names (`enginesVersion`, `productOutdated`, `lastSuccessfulScan`, `machineType`…).
- Your tenant's exact regional host; on-prem rate limit; the full Push event-type enumeration.

## Citations
- Public API (auth, JSON-RPC, rate limit): https://www.bitdefender.com/business/support/en/77209-125277-public-api.html
- Getting Started (API key, Basic auth): https://www.bitdefender.com/business/support/en/77211-125280-getting-started.html
- Network API: https://www.bitdefender.com/business/support/en/77211-128476-network.html
- Incidents API: https://www.bitdefender.com/business/support/en/77209-135326-incidents.html
- Push (Event Push Service): https://www.bitdefender.com/business/support/en/77212-135318-push.html
- On-prem API Guide (PDF): linked from the API documentation index.
