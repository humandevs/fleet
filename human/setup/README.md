# Integration setup guides

Operator-facing setup docs for each external integration in our UEM fork — how to obtain credentials,
what to paste where, and how the integration is wired. These pair with the *design* sections in
[../OSS.md §8](../OSS.md#8-integration-designs).

| Guide | Vendor | What it covers |
|-------|--------|----------------|
| [mosyle.md](./mosyle.md) | Mosyle (Apple MDM) | Getting the Mosyle API token; configuring device ingestion (primary Apple MDM path) |
| [action1.md](./action1.md) | Action1 (patch mgmt) | Instance-ID MSI agent push; REST API credentials for patch data/control |
| [bitdefender-gravityzone.md](./bitdefender-gravityzone.md) | Bitdefender GravityZone (AV/EDR) | GravityZone API key; endpoint/threat ingestion for the protection dashboard |
| [huntress.md](./huntress.md) | Huntress (EDR/MDR) | Huntress API key; agent/incident ingestion for the protection dashboard |
| [screenconnect.md](./screenconnect.md) | ConnectWise ScreenConnect (remote access) | Agent deploy + online status + Connect deep link; RESTful API Manager secret |
| [backups.md](./backups.md) | Veeam + iDrive360 (backups) | VSPC OAuth / iDrive360 MSP key; last-successful-backup status |

> 🔴 **SECURITY — do NOT store these secrets the "Jira mask-only" way.** The red-team verified
> ([RISK-REGISTER.md #3](../RISK-REGISTER.md)) that Fleet's `AppConfig.Integrations` pattern (`MaskedPassword`)
> only masks secrets in the API *response* — **at rest they are plaintext** in the `app_config_json` column
> (`SaveAppConfig` just `json.Marshal`s them; `server_private_key` encrypts only MDM certs, not AppConfig).
> A DB/backup dump = every client's vendor keys in cleartext. **These guides' "mask like Jira api_token"
> lines are superseded:** store each secret **envelope-encrypted in a dedicated secrets table with a
> KMS/HSM-backed per-tenant data key**, and **partition credentials per client/site** (the global
> `AppConfig.Integrations` singleton cannot hold per-tenant creds). **Never store the Mosyle admin
> password** — use token-only auth. This is a decide-now item; see RISK-REGISTER §1.2.

**Where these credentials are entered:** the admin **Settings → Integrations** UI. **Storage:** see the
security note above — a fork-owned encrypted per-tenant secrets store, *not* plaintext `app_config_json`.

> Each guide's **auth model, base URL, credential flow, and endpoint set are verified** against
> primary vendor docs (with an adversarial fact-check pass), and carry citations + a "verify against a
> live account" checklist. Exact JSON **field-name schemas** were behind JS-rendered Swaggers in a few
> cases and are flagged for live confirmation. Always re-check menu labels/endpoints against the live
> vendor console — they drift.
>
> Headline items to remember: **Bitdefender** = don't double `/api` in the base URL (deprecated SDK →
> use the Control Center JSON-RPC API); **Huntress** webhooks use **Svix** (don't hand-roll HMAC);
> **Action1** agent-download ID may differ from the API org ID; **Mosyle** needs *two* credentials
> (accessToken + admin login) and target the v1 Business flow first.
