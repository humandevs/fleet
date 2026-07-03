# Mosyle setup

Mosyle is our **primary Apple (iOS/iPadOS/macOS/tvOS) MDM authority**. This integration **ingests
Mosyle-managed devices into the product as hosts** (read/monitor) and can delegate a limited set of
Apple MDM actions back to Mosyle. Profile/app authoring stays in Mosyle. Design:
[../OSS.md §8.4](../OSS.md#84-mosyle-mdm-for-iosmacos).

> **Confidence: medium.** Mosyle's authoritative API reference is an authenticated in-app wiki (opened
> from **New Integration → Mosyle API Integration → documentation link**) that isn't publicly
> readable. The details below are corroborated from real community implementations (the SMillerDev
> Terraform provider, JCSmillie/MOSBasic) and multiple third-party integrator guides — **validate
> against a live Mosyle Business/Fuse tenant before shipping.**

## Which product / host

| Product | API host |
|---|---|
| **Mosyle Business / Fuse (ours)** | `https://businessapi.mosyle.com` |
| Mosyle Manager (edu) | `https://managerapi.mosyle.com` |

Don't hardcode the Manager host.

## Auth model — dual credentials (mandatory since 2024-02-08)

Every call needs **two** things working together:

1. **`accessToken`** — a long-lived per-integration **JWT** ("a Java Web Token with permission to fetch
   assets") generated in the Mosyle console. Identifies the integration.
2. **Admin email + password** — a Mosyle admin user with permission to fetch assets. *"Require User
   Credentials"* is **on by default and cannot be disabled** for new integrations, so the accessToken
   **alone is not sufficient**.

There are two API surfaces:

- **v1 (confirmed for the Business host)** — no login call. Each request:
  `POST https://businessapi.mosyle.com/v1/devices` (and `/v1/users`) with headers
  `accesstoken: <JWT>` + `Authorization: Basic base64(email:password)`, body
  `{"operation":"list","options":{…}}`. Response status `"OK"`, pagination `rows`/`page`/`page_size`.
- **v2 (confirmed on the Manager host; _unconfirmed on Business_)** — `POST /v2/login`
  `{accessToken,email,password}` → returns `Authorization: Bearer <sessionJWT>`; then `/v2/listdevices`
  and `/v2/bulkops` send **both** the `Bearer` header **and** `accessToken` repeated in the JSON body.

> **Decision:** target the **v1 Business flow** first (it's the confirmed one for our host). Probe
> whether `businessapi.mosyle.com` also serves the v2 `/v2/login → Bearer` paths against a live tenant;
> if it does, prefer v2 going forward. Do **not** assume the v2 Business paths exist.

## Get the credentials (Mosyle Business console)

1. **Create a dedicated admin API user** (role-restricted). Its email/password are sent on every call.
2. **Organization → API Integration** → enable the API profile → **Add new token** (top-right) → name
   it (e.g. `Fleet`) → optionally check **"Restricted by Server IP"** (pin to our integration server's
   egress IPs) → **copy the Access Token (JWT)** — shown once.
   - *(Note: the corroborated Business path is "Organization → API Integration". The nested
     "Integrations → Mosyle API Integration" wording is the **edu** path — verify the live label.)*
3. Record the admin **email + password** used, and store all three encrypted (see below).

## Product wiring

- New bounded context **`server/mdm/mosyle/`** (mirror `server/mdm/android/`): a typed client + cron
  poller + datastore. Client built on `fleethttp.NewClient()` (never raw `http.Client`), holding host +
  accessToken + admin creds; a `login()` that caches the Bearer (v2) and refreshes on 401.
- Config: a **Mosyle block** entered via **Settings → Integrations → Mosyle**. ⚠️ **Store the secrets
  envelope-encrypted in the fork's KMS-backed per-tenant secrets store — NOT plaintext in
  `app_config_json` / not the Jira mask-only pattern** ([RISK-REGISTER.md #3](../RISK-REGISTER.md)). Mosyle
  *mandates* the admin email+password on every call (a full Apple-MDM console credential — high value), and
  its "Require User Credentials" can't be disabled, so you can't go fully token-only. Mitigate: a
  **dedicated, least-privilege, IP-pinned** Mosyle admin whose password lives **only** in the KMS-encrypted
  store; cache the v2 Bearer to minimize password use; monitor + rotate.
- Poller (cron in `cmd/fleet/cron.go` + `server/service/schedule`): paginate `listdevices` per OS
  (`mac`/`ios`/`ipados`/`tvos`) until `DEVICES_NOTFOUND`; upsert hosts.
- **Host mapping:** key by **`serial_number` → `deviceudid`**. Store Mosyle-native fields
  (`managementstatus`, `osupdatestatus`, `date_checkin`/`date_last_beat`, supervision, assigned user).
- **MDM source:** Fleet already defines `fleet.WellKnownMDMMosyle = "Mosyle"` and maps the `"mosyle"`
  substring in an enrollment-server URL to it (`server/fleet/hosts.go:1366` and `:1387` via
  `MDMNameFromServerURL`). Set ingested hosts' `mdm.Name` to this so the UI shows Mosyle as the MDM
  authority — and so native-Fleet Apple devices vs. Mosyle-delegated devices never collide.
- **Delegated actions (optional, gate behind authz):** `POST /v2/bulkops` — `wipe_devices`,
  `change_to_limbo` (unenroll, often after `clear_commands`), `assign_device_user`, lost-mode/lock,
  restart. Wipe/limbo are destructive → require explicit operator confirmation.

## Data available (device fields)
`serial_number`, `deviceudid`, `os`, `osversion`, `buildversion`, `device_model(_name)`, `device_name`,
`is_supervised`, `managementstatus`, `enrollment_type`, `status`, `osupdatestatus`/`needosupdate`,
`date_last_beat`, `date_checkin`, `date_enroll`, disk/memory/cpu, MAC addresses, `imei`/`meid`, `tags`,
`asset_tag`, `userid`/`username`, `isactivationlockenabled`, `lostmode_status`, `is_deleted`.

## Gotchas
- **Two credentials required** — accessToken-only clients fail.
- **No published rate limit** — throttle conservatively, backoff on 429/5xx, honor `Retry-After` if present.
- Manual pagination (`page`/`page_size` in body; loop until `DEVICES_NOTFOUND`) — add a max-page guard.
- v2 Bearer lifetime unconfirmed (one client caches ~1h but comments say "good for 24hrs") — cache and
  refresh on 401 rather than asserting a fixed TTL.
- Server-IP restriction breaks polling if our egress IP changes — pin egress or leave unrestricted with
  compensating controls.
- Profile/app **authoring** via API appears limited — keep that in Mosyle/DDM, not pushed from us.
- No official Mosyle SDK — hand-roll the Go client.

## Verify against a live tenant before shipping
- Live Business-host **paths + version prefix** (v1 vs v2), and whether v2 calls need Bearer **and**
  body `accessToken`.
- Exact console labels; Bearer lifetime; rate limits; default/max `page_size`.
- Full set of supported push action names + option payloads.

## Citations
- Mosyle product/portal: https://mosyle.com/ · https://business.mosyle.com/
- Manager API host: https://managerapi.mosyle.com/
- Reference implementations (community, for modeling only): SMillerDev/terraform-provider-mosyle;
  JCSmillie/MOSBasic.
