# Action1 setup

Action1 is a cloud patch-management/RMM platform. Our fork integrates it in **two layers**:

- **(A) Agent deployment** — push the Action1 Windows agent to hosts using our **org-specific download
  URL**, via the existing software/script pipeline.
- **(B) Patch & vulnerability data + control** — call the Action1 REST API to read managed-endpoint
  status, missing updates, and CVE posture, and (optionally) trigger deployments — surfaced on a patch
  dashboard + per-host card.

Design: [../OSS.md §8.1](../OSS.md#81-software--patch-management).

> **Auth model & endpoints: confirmed** against Action1's official API docs and the first-party
> **PSAction1** PowerShell module (the authoritative machine-readable API surface). JSON request/response
> **schemas** (exact host-identity field names) are behind a JS-rendered Swagger — confirm those against
> a live tenant.

---

## (A) Deploy the Action1 agent

### 1. Get the org-specific download URL
In the Action1 console → **Endpoints → add endpoint / manual install**, copy the org-specific MSI URL:

```
https://app.action1.com/agent/<AGENT_DOWNLOAD_ID>/Windows/agent(My_Organization).msi
```

`<AGENT_DOWNLOAD_ID>` is a UUID (example: `292ad914-6cf4-11f1-8e22-a12cd5443bc6`).

> ⚠️ **Do not assume `<AGENT_DOWNLOAD_ID>` equals the API `ORG_ID`** (below). Action1's docs describe
> the agent-download id as "a unique ID associated with a downloadable agent setup," distinct from the
> organization UUID. Store it as its **own** config value and confirm the relationship in your console.
> Also: the MSI **filename after `/Windows/` may be validated** — use the exact URL the console emits.
> The download URL host may be **region-specific** for EU/AU orgs — confirm.

### 2. Deployment mechanism (orbit-run, as SYSTEM)
Model each Action1 agent install as a **software installer / script**. The orbit agent runs Windows
install scripts as SYSTEM (elevated, machine-wide — required; a non-elevated run silently fails).

Prefer the **URL-installer path** (`orbit DownloadSoftwareInstallerFromURL` → `msiexec`) over embedding
`curl` (older Windows lacks `curl`):

```powershell
# install script (ORG download URL templated from config)
msiexec /i "action1_agent.msi" /quiet /qn
```

Confirmed-working unattended one-liner (advanced/manual reference):

```cmd
curl -o "action1_agent(My_Organization).msi" "https://app.action1.com/agent/<AGENT_DOWNLOAD_ID>/Windows/agent(My_Organization).msi" && msiexec /i "action1_agent(My_Organization).msi" /quiet /qn
```

---

## (B) Action1 REST API — patch/vuln data & control

### Auth (OAuth2-branded client-credentials, **no `grant_type`**)
```
POST {BASE}/oauth2/token
body: client_id=<id>&client_secret=<secret>     # NO grant_type param
→ { "access_token": "<JWT>", "refresh_token": "...", "expires_in": 3600, "token_type": "bearer" }
```
Then send `Authorization: Bearer <access_token>` on every call. Cache the token; refresh ~5s before
expiry by **re-posting client_id/secret** (the first-party client ignores `refresh_token`).

> ⚠️ **Two gotchas:** (1) A generic OAuth2 `client_credentials` library will fail — there's no
> `grant_type`; post the fields directly. (2) The **official docs curl uses
> `Content-Type: application/x-www-form-urlencoded`**, but PSAction1 sends `application/json` — confirm
> which the endpoint accepts for your tenant.

### Regional base URLs (`{BASE}`)
| Region | Base URL |
|---|---|
| North America | `https://app.action1.com/api/3.0` |
| North America-2 | `https://app.na-2.action1.com/api/3.0` |
| Europe | `https://app.eu.action1.com/api/3.0` |
| Australia | `https://app.au.action1.com/api/3.0` |

### Get the credentials
Console → **Configuration → Users & API Credentials → + New API Credentials** → name it, pick a **role**
(role governs read vs. deploy scope) → **copy Client ID + Client Secret immediately** (secret is shown
**once**, non-recoverable — lose it and you recreate).

### Key endpoints (all org-scoped by `{ORG_ID}`)
| Purpose | Method |
|---|---|
| List accessible orgs / self | `GET {BASE}/organizations` · `GET {BASE}/Me` — resolve `{ORG_ID}` first |
| Managed endpoints (inventory + agent status) | `GET {BASE}/endpoints/managed/{ORG_ID}[/{ID}]` |
| Discovered endpoints (agent-deployment view) | `GET {BASE}/endpoints/discovery/{ORG_ID}` |
| Missing updates | `GET {BASE}/updates/{ORG_ID}` |
| Vulnerabilities (CVE posture) | `GET {BASE}/vulnerabilities/{ORG_ID}[/{CVE}[/endpoints|/remediations]]` |
| Trigger deployment (2-step) | `GET {BASE}/setting_templates/{ORG_ID}` then `POST {BASE}/policies/instances/{ORG_ID}` |
| CVE remediation | `POST {BASE}/vulnerabilities/{ORG_ID}/{CVE}/remediations` |
| Deploy results | `GET {BASE}/policies/instances/{ORG_ID}/{ID}/endpoint_results` |

**Deploy body** (from PSAction1 templates `deploy_package` / `deploy_update`):
`{name, retry_minutes, endpoints:[{id,type:"EndpointGroup"}], actions:[{name, template_id, params:{…}}]}`.
Verify end-to-end before enabling push.

### Rate limits
Stay **< 30 requests/minute** per Action1 Enterprise (429 counts across all endpoints). Official retry
guidance: on 429 wait **1s**, retry; if still 429 wait **1 min**; then run at lower frequency.

## Product wiring
- Client: `server/integrations/action1/` (or `server/service/externalsvc/action1.go`) — token
  cache + Bearer + per-region base URL + 429 backoff, on `fleethttp.NewClient()`.
- Config on `AppConfig.Integrations` (`server/fleet/integrations.go`): `region/base_url`, `client_id`,
  `client_secret` (encrypted), `default_org_id`, and the **`agent_download_id`** for Layer A.
- Sync worker (cron): pull `/endpoints/managed`, `/updates`, `/vulnerabilities`; upsert; map Action1
  endpoints → Fleet hosts by hostname/serial.
- UI: `PatchManagementPage` dashboard + per-host Action1 card.

## Verify against a live tenant
- `agent_download_id` vs API `ORG_ID` equivalence; region-specific agent download host.
- Token endpoint content-type; whether `refresh_token` is usable.
- JSON schemas / host-identity fields on `/endpoints/managed`, `/updates`, `/vulnerabilities` (pull the
  OpenAPI JSON from `app.action1.com/apidocs` in a browser).

## Citations
- API docs: https://www.action1.com/api-documentation/ (authentication, api-credentials, making-example-calls, how-action1-api-works)
- Manual agent install: https://www.action1.com/documentation/agent-installation/adding-endpoints-manually/win/
- First-party module (authoritative surface): https://github.com/Action1Corp/PSAction1
- Live Swagger: https://app.action1.com/apidocs/
