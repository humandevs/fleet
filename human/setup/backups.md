# Backups setup (Veeam + iDrive360)

Populates the coverage-matrix **"Backups"** column: *is a backup agent installed, is it healthy, when was
the last successful backup.* Both products expose a central REST API **and** a readable local footprint, so
a central poller is preferred with an osquery fallback (strong for Veeam, weak for iDrive360). Design:
[../MVP.md](../MVP.md).

> Confidence: medium. Auth + base URLs + primary endpoints verified against official docs; exact JSON
> field names for "last successful backup" are behind JS-rendered Swaggers — confirm against a live API.

## Shared model
Build **one** `Backups` host-detail schema and feed it from either poller:
`{ agent_installed bool, agent_healthy bool, last_successful_backup timestamp, source enum(veeam|idrive) }`.
Match console records to Fleet hosts by **hostname → hardware_serial**.

## Veeam

**A) Veeam Service Provider Console (VSPC) — best for a fleet-wide/MSP poller.** OAuth2:
```
POST https://<vspc-host>:1280/api/v3/token
Content-Type: application/x-www-form-urlencoded
grant_type=password&username=<domain\user>&password=<pw>     → access_token + refresh_token
```
Then `Authorization: Bearer <access_token>`; refresh via `grant_type=refresh_token` (tokens expire — build
refresh in). Also supports API keys / asymmetric JWT.
- List agents: `GET /api/v3/infrastructure/backupAgents` (installed + health/license).
- Per-agent jobs + last run: `GET /api/v3/backupAgents/{uid}/jobs` (status / last-session / last-result).
- Credential: a VSPC deployment + a service-provider/company-admin (or restricted API) account; host + port **1280**.

**B) Veeam Backup Enterprise Manager — per-VBR-site alternative (single tenant).** Session token, not OAuth:
`POST /api/sessionMngr/?v=latest` with **Basic** auth → token in the **`X-RestSvcSessionId`** response header,
sent on later calls; **15-min idle** timeout. (Don't confuse with the newer VBR REST API on port **9419**,
`POST /api/oauth2/token`.)

**C) osquery fallback (standalone/unmanaged Veeam Agent).** Works without any API:
- Installed: `services` name `Veeam.Endpoint.Service` (display "Veeam Agent for Microsoft Windows"; older
  "Veeam Endpoint Backup Service" — verify with a live `services` query).
- Health/last backup: `windows_eventlog` WHERE source = `Veeam Agent` → event **190 "Backup Job Finished"**
  (read **severity**: Information = success, Warning/Error = problem); 110 started, 191 retry. A missing recent
  190 = stale/failed. Config: registry `HKLM\SOFTWARE\Veeam\Veeam Endpoint Backup`.

## iDrive360

**MSP REST API — the realistic path.** Static API key as bearer:
```
Authorization: Bearer <API-KEY>      (base: https://api.idrive360.com)
```
- Devices + status + last backup: `GET /api/msp/device/summary` — returns device status
  (online/offline/blocked/archived), **last backup timestamp**, next scheduled backup. **The single most
  useful call.**
- Tenants: `GET /api/msp/company` (iterate companies → device/summary per company).
- Credential: an iDrive360 **MSP/centralized-console** account → Management Console → **My Account → API Keys**
  → generate. (A plain consumer iDrive account has no MSP API.) Treat the key as a high-value secret.
- osquery fallback is **best-effort only** (service "IDrive 360 Service" + process + files under
  `C:\ProgramData\IDrive360`) — last-successful-backup time isn't in a cleanly parseable local value.

## Product wiring
- Two cron pollers (`server/integrations/veeam/`, `server/integrations/idrive360/`) → the shared `Backups`
  host-detail (`HostStatusProvider`, [PLUGINS.md](../PLUGINS.md)); optional Veeam Event-Log osquery ingest
  for standalone agents.
- Config (base URL/host+port, creds/API key **encrypted**) in `AppConfig.Integrations`.

## Verify against live APIs
- VSPC job resource field name for last-successful-backup / last-result / status.
- iDrive360 `/device/summary` last-backup field key + success-vs-failure representation; MSP-tier availability + rate limits.
- Whether the estate is VSPC-managed vs standalone Veeam Agent vs VBR+EM (decides API vs Event-Log path).

## Citations
- VSPC REST (auth v3, port 1280, backup agents/jobs): helpcenter.veeam.com/docs/vac/rest/ (about, authorization_example_v3, oauth, agentjob_prprts)
- Veeam EM REST (session token): helpcenter.veeam.com/docs/vbr/em_rest/ (http_authentication, logging_on)
- Veeam Agent event IDs: helpcenter.veeam.com/docs/agentforwindows/userguide/appendix_events.html
- iDrive360 MSP API: idrive.com/endpoint-backup/api-collections
