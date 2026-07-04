# ScreenConnect setup

ConnectWise ScreenConnect ("Control") is our **remote-access** tool. Integration = three pieces:
**(1)** deploy the unattended access agent to managed hosts (tagged into the right client/site),
**(2)** read per-device online / last-connected status for the coverage-matrix "Remote Access" column,
**(3)** a one-click **Connect** deep link from the host page. Design:
[../MVP.md](../MVP.md) · [../OSS.md §8.2](../OSS.md#82-avedr-dashboards-bitdefender-gravityzone--huntress).

> Confidence: medium. Auth + install mechanics corroborated from ConnectWise docs + widely-used
> community modules (Labtech/JustinGrote); exact RESTful API Manager method paths are behind an
> authenticated portal — verify against your installed extension.

## Programmatic surfaces (pick the stable one)
- **RESTful API Manager extension (recommended, supported).** Install it from the Extension Marketplace
  (Admin → Extensions), set a long random `RESTfulAuthenticationSecret`, and send it as the
  **`CTRLAuthHeader`** header; optionally lock `RESTfulAllowedOrigin`. GET/POST JSON. The shared secret
  **is** the credential (no OAuth). Endpoints live under `/App_Extensions/<extension-GUID>/Service.ashx/<Method>`.
- **Internal `/Services/PageService.ashx/*` (fragile, avoid as primary).** Undocumented JSON-RPC the web
  Host page uses; HTTP Basic + (newer builds) an `x-anti-forgery-token` + cookie. Changes across versions.
- **Session Manager / Report Manager API.** Server-side object model (`Session.GuestConnectedCount`,
  `ActiveConnections`, `CustomProperty1..8`); surface it externally by wrapping in the RESTful API Manager.

Agent enrollment itself needs **no** API credential — the per-org installer embeds the instance URL,
relay/thumbprint, and join key, so an installed agent auto-registers.

## Deploy the access agent (via orbit script pipeline)

### Windows (orbit runs as SYSTEM)
URL-baked installer — the repeated `&c=` values populate `CustomProperty1..8` (defaults: **Company, Site,
Department, Device Type** — this is the **client/site handoff**), `t=` sets the session name:
```
https://<instance>/Bin/<Name>.ClientSetup.msi?e=Access&y=Guest&t=<FleetHostname>&c=<Client>&c=<Site>&c=<Dept>&c=<DeviceType>
```
```cmd
msiexec /i "ScreenConnect.ClientSetup.msi" /qn /norestart REBOOT=REALLYSUPPRESS /l*v "%WINDIR%\Temp\sc-install.log"
```
(Generic-MSI alternative: pass `SERVICE_CLIENT_LAUNCH_PARAMETERS` holding the `c=&c=` launch string.)
**Capture the SessionID** afterward (durable Fleet-host ↔ ScreenConnect key): read the ScreenConnect
Client service's `ImagePath` under `HKLM\SYSTEM\CurrentControlSet\Services\` — it contains `s=<SessionID GUID>`.
Store that GUID + the Company/Site as Fleet host attributes.

### macOS (orbit runs as root)
```bash
sudo installer -pkg ScreenConnect.ClientSetup.pkg -target /
```
⚠️ **The Mac pkg is not signed/notarized for hands-off deploy.** The agent installs but **can't control
the screen** until TCC/PPPC grants exist. **Push a PPPC/TCC config profile via Fleet MDM** (Accessibility +
Screen Recording + Full Disk Access) *before/with* the install, or the deploy looks successful but is inert.

## Status poller (coverage-matrix "Remote Access" signal)

> **Implemented.** `screenconnect.Provider.Collect` (a community `Collector`) polls the RESTful API Manager
> and returns `remote_access` reports keyed by session name (= the Fleet hostname we set via `t=` at
> install); the community `Runner` resolves them to hosts and upserts cells. Config it with `AccessSecret`
> (the `CTRLAuthHeader` secret) + `APIPath` (your installed extension's session-list method path —
> instance-specific, so it's configured not hardcoded). Leave both empty for deployment-only mode.

A Go cron calls the RESTful API Manager (`GetSessionsByFilter`, header `CTRLAuthHeader`) — or, fallback,
`POST /Services/PageService.ashx/GetHostSessionInfo` (Basic auth) — filtered by the site custom property
or session name. Derive:
- **Online now** = `GuestConnectedCount > 0` or non-empty `ActiveConnections`.
- **Last connected** = latest connect/disconnect event via `GetSessionDetails`.
Persist a per-host `remote_access` status row keyed by the stored **SessionID** (preferred) → hostname.
Poll on the order of **minutes**, one bulk `GetSessions` call, not per-host.

## Group provisioning (Fleet clients/sites → ScreenConnect groups)

Two separate mechanisms, don't conflate them:
- **Routing (device → group)** happens entirely via the installer's `&c=` values. `CustomProperty1..8` are
  positional; by convention **CustomProperty1 = client** (the group key), **CustomProperty2 = site**, then
  department / device type. Set them per host at install (`screenconnect.OrgLink`); nothing server-side is
  required for the device to carry the tags.
- **Grouping (UI view)** is a saved **session-group filter** (e.g. `CustomProperty1 = 'Acme'`) that must
  exist for the ScreenConnect UI to *show* those tagged devices as a group. This is what we'd "push" when a
  Fleet client/site is created.

**Push-on-create design.** Hook the Fleet client (team) / site create+rename lifecycle → call
`screenconnect.Provider.EnsureSessionGroup(ctx, client, sites...)`, which idempotently ensures a group
filtered on `CustomProperty1 = '<client>'` (+ a child group per site adding `CustomProperty2 = '<site>'`).

> Confidence: **low** for programmatic group CRUD. The RESTful API Manager exposes session read/command
> methods, but session-**group** management is an admin/config surface. Preference order: **(1)** a supported
> RESTful API Manager method if the installed extension exposes SessionGroup CRUD; **(2)** since we
> self-host, manage the server config directly (write `SessionGroup` elements); **(3)** internal page
> service (version-fragile) only as a last resort. `EnsureSessionGroup` is a wired seam (no-op) today.

## Connect button (deep link)
```
https://<instance>/Host#Access/<GroupName>//<SessionID>/Join
```
Note the **double slash** between group and SessionID. It's a **browser deep link, not an API call** — it
needs an authenticated ScreenConnect Host browser session (first click prompts SSO/login).

## Product wiring
- **Deploy** = orbit install script (existing software-install pipeline), installer URL templated with the
  host's team (client) + site into `&c=` and `t=<hostname>`.
- **Status** = cron poller → `host_integration_status.remote_access` (the [PLUGINS.md](../PLUGINS.md)
  `HostStatusProvider` pattern).
- **Config** = ScreenConnect **base URL** (`InstanceURL` — required; self-hosted has no `*.screenconnect.com`
  default) + `CTRLAuthHeader` secret in `AppConfig.Integrations`.
  - **Self-hosted, standard:** point `InstanceURL` at your own server (e.g. `https://remote.example.com` or
    `https://example.com:8040`). The per-org installer downloaded from it bakes in that server's relay
    address + cert thumbprint + join key, so nothing else is needed.
  - **Self-hosted, reverse-proxied (web on 443, relay on host:8041):** an installer pulled through the 443
    proxy would bake in the *proxy* as its relay. Override the phone-home endpoint with `RelayHost` +
    `RelayPort` (installer `h=`/`p=`) and, for a self-signed cert, `Thumbprint` (installer `k=`).
- **UI** = "Remote Access" coverage-matrix icon + a "Connect" button opening the Join link.

## Gotchas
- **SessionID (GUID) ≠ Fleet host UUID** — capture the mapping at install (Win: service ImagePath; mac: app config).
- Internal `/Services` endpoints are undocumented and version-fragile — don't build on them primarily.
- Only **8** custom-property fields; `&c=` maps **positionally** — keep order stable between install and poller filter.
- Harden post-**CVE-2024-1709** (auth bypass): patch current, restrict `RESTfulAuthenticationSecret`, lock
  `RESTfulAllowedOrigin`, least-privilege host user.

## Citations
- Session Manager API: https://docs.connectwise.com/ScreenConnect_Documentation/Developers/Session_Manager_API_Reference
- RESTful API Manager: https://docs.connectwise.com/ScreenConnect_Documentation/Developers/RESTful_API_Manager
- Build an access agent installer: https://docs.connectwise.com/ScreenConnect_Documentation/Get_started/Host_page/Build_an_access_agent_installer
- Community deploy + launch-URL refs: ninjaone.com script-hub (ScreenConnect deployment / launch URLs); docs.tacticalrmm.com/3rdparty_screenconnect/
