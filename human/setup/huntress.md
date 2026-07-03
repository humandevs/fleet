# Huntress setup

Read-only ingestion of **Huntress managed-EDR** posture — per host: agent installed/healthy (version,
last survey, OS) and open incident reports (status, severity, remediation) — for the Endpoint Protection
dashboard + a per-host card. Design:
[../OSS.md §8.2](../OSS.md#82-avedr-dashboards-bitdefender-gravityzone--huntress).

> **Auth model, base URL, credential flow, endpoint set, and rate limit: confirmed** against Huntress's
> official docs + changelog and reputable integrator references. Exact JSON **field names** and the
> pagination envelope live behind a JS-rendered Swagger — confirm against a live account before coding
> deserialization structs.

## Auth model — HTTP Basic (public key : secret key)
```
Authorization: Basic base64(hk_… : hs_…)     # username = API Key (public, hk_), password = Secret (hs_)
```
Go: `req.SetBasicAuth(publicKey, secretKey)`. No OAuth/token/refresh; credentials are long-lived until
revoked. **Base URL: `https://api.huntress.io/v1`** (include the `/v1`).

**Key permissions mirror the creating user's role** — create a **dedicated low-privilege service user**
for monitoring. (Incident resolution needs a user with that permission.)

## Get the credentials
Huntress Portal (as an **Admin** user) → **hamburger menu (upper-right) → API Credentials** (or
**Account Settings → API Credentials**) → **User API Credentials → Add** → select the service user →
enter a required **Key Name** → **Create** → copy the **API Key (`hk_…`)** and **API Secret (`hs_…`)**
— the secret is shown **once** (delete + recreate if lost).

## Endpoints (v1)
| Purpose | Method |
|---|---|
| Health / auth check | `GET /v1/account` *(confirm exact path live)* |
| Organizations (tenants) | `GET /v1/organizations[/{id}]` |
| Agents (installed endpoints) | `GET /v1/agents[/{id}]` |
| Incident reports (detections) | `GET /v1/incident_reports[/{id}]` |
| Resolve an incident (push action, optional) | `POST /v1/incident_reports/{id}/resolution` *(third-party-confirmed — verify live)* |
| Summary / billing reports | `GET /v1/summary_reports` · `GET /v1/billing_reports` |

**Rate limit: 60 requests/minute** (sliding window) — poll every 15–30 min and back off on 429.

## Real-time incidents — use **Svix** (don't hand-roll HMAC)
Huntress webhooks are signed with **Svix**. Verify with the **Svix Go library**
(`github.com/svix/svix-webhooks/go`) using the standard `svix-id` / `svix-timestamp` / `svix-signature`
headers (HMAC-SHA256 over the **raw** request body — re-serializing the JSON breaks the signature). This
supersedes any "hand-rolled HMAC receiver" idea. Webhook setup is under the portal **Integrations**
section (account-admin), with **Send Test** / **View Delivery Attempts**.

## Product wiring
- Client: `server/service/externalsvc/huntress.go` (mirror `jira.go`): `Huntress{BaseURL, PublicKey,
  SecretKey}`, `fleethttp.NewClient()`, `req.SetBasicAuth` per call, reuse `doWithRetry` (maxRetries=5,
  retryBackoff=300ms) to honor 429 + `Retry-After`. Add a `HuntressConfigMatches` helper.
- Config: a **singleton `*HuntressIntegration`** on `AppConfig.Integrations` (`Enabled`, `BaseURL`
  default `https://api.huntress.io/v1`, `APIKey`, `APISecret`) — mask `APIKey`/`APISecret` as
  `fleet.MaskedPassword` in AppConfig marshaling (`app.go` ~774) and handle deep-copy in `AppConfig.Copy`
  (~886), like Jira `api_token`. **Settings → Integrations → Huntress.**
- Cron poller (`cmd/fleet/cron.go`, re-read `AppConfig` each run like `integrations_worker`): page
  `/v1/agents` + `/v1/incident_reports`, upsert.
- **Host mapping by hostname only** (no shared UUID). New table `host_huntress_agents` keyed by
  `host_id` (`huntress_agent_id`, org id/name, `agent_version`, `last_survey_at`, rolled-up
  incident status/severity); resolve via `ds.HostByIdentifier`. Keep **unmatched agents** in a side
  table for a "Huntress agents with no Fleet host" view. *(Run `go test ./server/service/` after adding
  Datastore interface methods — mocks.)*
- Host-detail "Huntress" card + dashboard summary card.

## Gotchas
- Secret shown once; store server-side only (never ships to frontend).
- Hostname-only correlation → handle renames, case differences, one-sided hosts gracefully.
- Don't confuse the cloud API (`api.huntress.io`) with the per-endpoint **localhost EDR Agent Health
  API** (`http://localhost:24799/health`) — the latter is not usable by our central server.

## Verify against a live account
- Exact agent field names (`last_survey_at` vs `last_callback_at`…), incident status/severity enums.
- Pagination parameter + response envelope names (`page` is confirmed to exist; the rest isn't).
- The resolve-incident path/body; the `GET /v1/account` health path.

## Citations
- REST API Overview: https://support.huntress.io/hc/en-us/articles/4780697192851-Huntress-REST-API-Overview
- Generating API Keys: https://support.huntress.io/hc/en-us/articles/4416826761235-Generating-API-Keys
- API is in Public Beta (endpoint set): https://www.huntress.com/blog/huntress-api-is-now-in-public-beta
- Incident-response API: https://feedback.huntress.com/changelog/apis-for-escalations-and-incident-report-responses-now-available
- Webhooks (Svix): https://support.huntress.io/hc/en-us/articles/51679111552147-How-to-use-Webhooks-Integration
- Interactive docs / OpenAPI: https://api.huntress.io/docs
