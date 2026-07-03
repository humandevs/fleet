# ZERO-TRUST.md — deployment & network security model

> How we run the fork's server so its admin surface is locked down (it has admin over every managed
> device) while the agent/MDM device plane stays reachable. Companion to [`OSS.md`](./OSS.md),
> [`MVP.md`](./MVP.md), [`CONFIG-MGMT.md`](./CONFIG-MGMT.md). Cloudflare specifics verified against official
> docs; route inventory verified against the repo.

## The one idea: two planes, secured oppositely

Fleet mounts **everything on one listener** (one server URL). You **cannot** split cleanly at "`/api`
behind the gateway, `/` open" — **both planes live under `/api/`**. The split is by **explicit path
prefix**, and the two planes get opposite treatment:

| Plane | What | Reachability | Security |
|---|---|---|---|
| **Device plane** | osquery/orbit/fleetd, Fleet Desktop, Apple MDM+SCEP, Windows MDE2, Android Pub/Sub, enroll/OTA, installer-token downloads, SCIM, MDM-SSO | **Must stay public** to devices & external callers | **App-layer only** (enroll secret, node_key, device token, SCEP/MDM cert). Optional Cloudflare **proxy + WAF + rate-limit — but NO identity gate** |
| **Admin plane** | Web UI, admin `/api/_version_/fleet/*`, `/debug`, `/metrics`, API-only integration tokens | Restricted | **Cloudflare Tunnel** (no public inbound ports) + **Access** (SSO + device posture); **service tokens** for `fleetctl`/CI |

Why the device plane can't sit behind Access: Access issues an **interactive SSO 302** that a machine
agent, the MDM protocol, or an Apple/Google push callback **can never complete**.

## Device plane — the exact prefixes to keep public (BYPASS Access)

Verified from `server/service/handler.go` + `cmd/fleet/serve.go` + the MDM path constants:

- `/api/osquery/*`, `/api/v1/osquery/*` — osquery TLS (node_key / enroll_secret)
- `/api/fleet/orbit/*` — orbit (enroll_secret → orbit_node_key)
- `/api/fleetd/certificates/*` — fleetd cert templates
- `/api/_version_/fleet/device/*` — Fleet Desktop (per-device token)
- `/mdm/apple/scep`, `/mdm/apple/mdm`, `/mdm/apple/service_discovery*`, `/mdm/scep/proxy/*` — Apple MDM protocol (mounted on rootMux, **not** under `/api`)
- `/api/mdm/apple/*` — Apple enroll / installer / OTA / account-driven / EULA / bootstrap
- `/api/mdm/microsoft/*` — Windows MS-MDE2 (discovery/auth/policy/enroll/management/tos)
- `/api/v1/fleet/android_enterprise/pubsub`, `.../enrollment_token`, `.../connect/*` — Android (see below)
- `/api/_version_/fleet/ota_enrollment`, `/enrollment_profiles/*`, `/enroll`, `/assets/*`
- `/api/_version_/fleet/mdm/sso*` — **device-driven** enrollment SSO (looks admin, is device)
- `/api/*/fleet/scim*` — inbound IdP provisioning (Okta/Entra), webhook-like
- `/api/_version_/fleet/software/titles/*/package/token/*`, `.../in_house_app/*` — signed-token downloads

**Everything else under `/api/_version_/fleet/*` = admin plane → ENFORCE Access.** Order the Cloudflare
rules so these **device prefixes Bypass *before*** the broad admin Enforce catch-all.

### Three device-plane endpoints that surprise people
- **Android Pub/Sub webhook** (`POST /api/v1/fleet/android_enterprise/pubsub?token=`) is **inbound from
  Google Cloud** (source IPs you don't control), authed only by the `?token=`. Public + WAF/rate-limit only —
  can't be Access-gated or IP-allowlisted.
- **SCIM** — inbound from your IdP; treat like a webhook (public + token).
- **MDM enrollment SSO** — a device browser flow during enrollment; must be public even though it reads like admin SSO.

## Admin plane — Cloudflare Tunnel + Access

- **`cloudflared` Tunnel**: outbound-only (port 7844), **no public inbound ports**, origin unreachable
  except through the tunnel. Publishes the admin hostname as a self-hosted Access app.
- **Access**: deny-by-default. **Humans** → SSO (your IdP) + optional device posture (**require WARP** or
  **client-certificate**). **Machines** (`fleetctl`, GitOps CI, monitoring) → a **Service Auth** policy +
  **service token**, sending `CF-Access-Client-Id` / `CF-Access-Client-Secret` headers.
- ⚠️ **api-only integration tokens break behind Access unless** each runner also sends the CF-Access service-token
  headers *in addition to* its Fleet bearer token. Easy to forget.
- Enforce Access on: `/`, `/debug/*`, `/metrics`, and the admin `/api/_version_/fleet/*` set. Keep `/assets/*`
  **public** (shared with the `/enroll` device page).

## Protecting the (public) device plane with Cloudflare — without breaking it

Put the device hostname behind Cloudflare's orange-cloud proxy for WAF/DDoS/rate-limit, with these
non-negotiable tunings (verified):

1. **Skip the injection WAF on agent/MDM paths.** osquery bodies contain SQL/PowerShell/odd software names;
   plist + PKCS#7 MDM bodies look like attacks. The OWASP SQLi/RCE managed rules **will 403 legitimate
   check-ins**. Add a **WAF exception (skip rule)** scoped to the device hostname/paths, placed *before* the
   managed execute rules. Allow the MDM content types (`application/x-apple-aspen-mdm`, `application/pkcs7-*`).
2. **Use API Shield instead** — schema validation (positive security from an OpenAPI spec) + per-endpoint
   rate limiting + optional mTLS/JWT is the right model for a machine-to-machine API.
3. **Lock the origin** — **cloudflared Tunnel** (best; no public ports) or **Authenticated Origin Pulls
   (zone/per-hostname mTLS)** + a secret header. IP-allowlisting Cloudflare's ranges alone is weak (shared
   across all CF tenants).
4. **Body-size caps** — Free/Pro **100 MB**, Business **200 MB**, Enterprise **500 MB** (default). Large
   osquery log posts and **software-installer up/downloads can 413** → route big transfers through a
   grey-cloud hostname / Tunnel / object storage, or use Enterprise.
5. **WebSockets** — enable (live-query UI streams over WS; agents use HTTP long-poll, not WS).

### The good news on Apple MDM (verified in-repo)
**Fleet's Apple MDM does NOT use TLS-layer client certificates** — it sets `SignMessage=true`
(`server/mdm/apple/apple_mdm.go:1183`) and NanoMDM extracts the device identity cert from the
**`Mdm-Signature` HTTP header** (`server/mdm/nanomdm/http/mdm/mdm_cert.go:104-163`), Apple's "pass an
identity cert through a proxy" design. That header isn't in Cloudflare's protected namespace, so **Apple MDM
check-in proxies through the orange cloud cleanly — no edge mTLS, no grey-clouding** — you just skip the WAF
on MDM paths. *(One still-unverified endpoint: the Windows OMA-DM management/sync channel — confirm its auth
before proxying; if it uses TLS client certs, use API-Shield edge-mTLS + RFC 9440 cert forwarding, or
grey-cloud just that path.)*

## WARP — device-wide, and only on technician machines

**Definitive (verified):** WARP enrolls the **DEVICE**, not a per-user login. Push `organization` + a
**service token** (`auth_client_id`/`auth_client_secret` with Service-Auth device-enrollment permission) and
the device enrolls with **no user interaction** (shows as `non_identity@<team>.cloudflareaccess.com`).
`multi_user=false` (default) = one registration per device; `multi_user=true` is a Windows-only per-user opt-in.

**Deploy WARP only to technician/admin machines** and require it as an Access posture check on the admin
plane. **Do NOT put WARP on every managed endpoint:** the agents already mutually authenticate, iOS/Android/
BYOD can't run WARP as a device agent, and tunneling every device would break the public reachability the
device plane needs. (Pushing WARP *config* per client/site = a Fleet script writing `mdm.xml`, **not** a
registry key — see [CONFIG-MGMT.md](./CONFIG-MGMT.md#worked-example-warp-config-per-clientsite).) WARP
*install/enroll status* is a coverage-matrix column ([MVP.md](./MVP.md)).

## Two non-obvious constraints
- **The server URL is effectively immutable.** It's baked into MDM enrollment profiles, SCEP URLs, orbit/
  osquery config, and OTA discovery — so **it must be the public device hostname.** An admin hostname is only
  an *alternate ingress* to the same backend. (Prefer a single-hostname path-policy split over two hostnames.)
- **Agent auto-update (TUF) is a separate public origin** — not served by the Fleet binary. Your fork's
  fleetd pulls from a configured TUF URL (default `updates.fleetdm.com`); that mirror must stay public to
  agents and outside Access. Plan its hosting independently (see OSS.md §7).

## AWS alternative
**AWS API Gateway** (+ WAF, mutual-TLS custom domain, IAM/Lambda authorizer, private VPC link) is a valid
admin-plane alternative — better if you're already all-in on AWS or specifically want edge client-cert mTLS /
a fully-private VPC API. It **lacks Cloudflare's built-in SSO + device-posture** layer (you'd assemble
Cognito/OIDC/Lambda authorizer), and mutual TLS is **not** supported on private APIs (edge-mTLS *or* private,
not both). For turnkey SSO + posture with least assembly, **Cloudflare Access wins**.

## Recommended split (single hostname, path policy)
1. Keep the fork on **one public hostname** (the immutable device URL).
2. Cloudflare Access app on `/` + `/debug/*` + `/metrics` + admin `/api/_version_/fleet/*` → **Enforce** (SSO
   + posture; service tokens for CI).
3. **Bypass** (public) the device-plane prefix list above — ordered *before* the admin Enforce.
4. Device plane also gets **WAF/rate-limit** (injection rules skipped) + Authenticated Origin Pulls / Tunnel.
5. WARP on technician machines only; required as admin-plane posture.
6. TUF update mirror = separate public origin.

## Verify before locking down
- The live device-prefix list against your fork's `handler.go` (paths drift).
- Windows OMA-DM sync-channel auth (TLS client cert?) before proxying it.
- Cloudflare body-size cap vs your largest installer/log post; Business-plan cap (200 MB) on your account.
- Exact Cloudflare console labels (Access apps, service tokens, AOP, WAF exceptions).

## Citations
- Cloudflare: Tunnel, Access self-hosted apps, service tokens, WARP MDM parameters + device enrollment, WAF
  exceptions, API Shield (schema validation/mTLS/JWT), Authenticated Origin Pulls, Error 413 body limits —
  developers.cloudflare.com (see per-topic URLs captured in the research pass).
- AWS: API Gateway mutual TLS + WAF — docs.aws.amazon.com/apigateway.
- Repo: `server/service/handler.go` (route/endpointer inventory), `server/mdm/apple/apple_mdm.go:1183`,
  `server/mdm/nanomdm/http/mdm/mdm_cert.go:104-163`, `server/mdm/microsoft/microsoft_mdm.go`,
  `server/mdm/android/service/handler.go`, `cmd/fleet/serve.go`.
