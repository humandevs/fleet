# CONFIG-MGMT.md — "DevOps for endpoints" (desired-state config management)

> Decision doc for how our fork does endpoint configuration management. Companion to
> [`OSS.md`](./OSS.md), [`PLUGINS.md`](./PLUGINS.md), and [`ZERO-TRUST.md`](./ZERO-TRUST.md).
> Worked example throughout: pushing **Cloudflare WARP** config per client/site.

## TL;DR decision

- **Do NOT run a SaltStack master (or `salt-ssh`, or Ansible push, or any inbound-push model).** A second
  control plane + inbound channel on every endpoint directly violates the zero-trust device-plane goal,
  doubles the privileged-agent attack surface, and gives two sources of truth. (Salt's master RCE history
  makes this worse for a security product.)
- **Fleet is the single control plane and scheduler.** Config management runs as an **executor invoked
  locally by the orbit agent** — no daemon, no listener.
- **Default engine = Fleet-native** (GitOps declares → config profile / script applies → policy detects
  drift → policy-automation re-applies). This *is* the endpoint-DevOps loop, built on MIT-core primitives.
- **Richer Windows desired-state = PowerShell DSC v3** (a standalone Rust CLI `dsc.exe`, no daemon, no
  MOF, GA'd 2025) invoked by an orbit script. Optional cross-platform idempotent language = **masterless
  `salt-call --local`** if you're willing to ship the Salt runtime as content.
- **Apple settings = config profiles + DDM** (natively declarative/self-healing).
- **Not Ansible-pull on Windows** — Ansible's controller doesn't run natively on Windows.

## The "DevOps for endpoints" convergence loop

Fleet already gives four of the five config-management primitives; the fifth (a real desired-state engine)
is bolted on *as a local executor*, not a second control plane:

```
GitOps (declare desired state, per team/site)         ← config-as-code, the "DevOps" layer
        │
        ▼
Config profile (Apple/Windows CSP)  ── apply declaratively
Script (orbit, runs as SYSTEM/root) ── apply imperatively / invoke DSC or salt-call --local
        │
        ▼
Policy (osquery boolean: "is it in the declared state?")  ── detect drift  (~hourly re-eval)
        │  fail
        ▼
Policy automation → run_script  ── converge (re-apply). Retries capped (MaxPolicyAutomationRetries=3)
```

**The clean pattern:** a Fleet policy detects drift (boolean), its automation runs a script, and **the
script invokes `dsc config set` (or `salt-call --local state.apply`) against a state file delivered by
GitOps**. Fleet stays the sole control plane + scheduler; DSC/Salt is just the local idempotent executor.
Best of both — one agent, one source of truth, GitOps-declared, drift-checked.

**Where Fleet-native alone suffices:** flat, boolean-checkable settings and one-shot remediation (a
registry value present/absent, a service running, a file/hash present, "app installed"); Apple settings
via profiles/DDM. **Where it needs a real engine:** idempotent multi-value convergence, ordered/dependent
changes, whole registry subtrees, templated files, "ensure exactly this and re-apply on drift within
minutes." That's the DSC-v3 / Salt gap.

## Engine comparison (as an orbit-invoked, no-daemon executor)

| Need | Use | Notes |
|---|---|---|
| Flat setting, boolean-checkable, one-shot fix | **Fleet-native** (policy → script), GitOps-declared | Policies are boolean-only (`≥1 row` = pass); scripts run as SYSTEM (`orbit/pkg/scripts/exec_windows.go:17` `powershell -ExecutionPolicy Bypass`) but are **not idempotent** and have no ordering graph |
| Apple (macOS/iOS) settings | **Fleet config profiles + DDM** | Truly declarative + self-healing on Apple |
| Rich Windows desired-state (registry trees, services, files, pkgs) | **DSC v3** invoked by an orbit script (`dsc config test`→`set`) | Standalone Rust CLI, no daemon, cross-platform, GA v3.0 (Mar 2025) / v3.2 (Apr 2026); reuses existing PSDSC resources via its PowerShell adapter |
| One idempotent language across Win+mac+Linux | **masterless `salt-call --local`** | True no-master/no-minion (`file_client: local`); reuses Salt *formulas*; heaviest option (ships Python Salt runtime); Windows-masterless has rough edges (winrepo, GPO can override registry) |
| Windows-node local pull execution | **Not Ansible-pull** | Ansible controller doesn't run natively on Windows |
| Any "master"/inbound channel | **Never** | No Salt master, no `salt-ssh`, no WinRM push — violates zero-trust device plane |

**Content reuse:** Salt *formulas* → reusable if you adopt masterless Salt. PSDSC/DSC resources → reusable
via DSC v3's PowerShell adapter. Ansible roles → effectively **not** reusable for Windows nodes. Fleet
GitOps YAML is the system-of-record that points at whichever engine's state files.

**Recommendation:** start Fleet-native (covers most MSP settings). Add **DSC v3** as the orbit-invoked
Windows engine the first time you hit a multi-value/registry-tree requirement. Consider masterless Salt
only if you specifically want one cross-platform state language and accept the runtime weight.

> ⚠️ Caveat to verify: the claim "Intune Settings Catalog uses DSC v3 under the hood" is **overstated** —
> Azure Machine Configuration uses DSC v3, but Intune's Windows "Declared Configuration" still uses MOF
> fragments. Don't repeat the Settings-Catalog=DSC-v3 claim as fact.

## Worked example: WARP config per client/site

### It's `mdm.xml`, not a registry key (correction)
WARP does **not** read managed config from `HKLM\SOFTWARE\Cloudflare\...` or any OMA-URI. It reads:
- **Windows:** `C:\ProgramData\Cloudflare\mdm.xml` (processed immediately on change), or MSI properties at
  install time (`ORGANIZATION`, `AUTH_CLIENT_ID`, `AUTH_CLIENT_SECRET`, `SERVICE_MODE`, `SWITCH_LOCKED`, …).
- **macOS:** a config profile delivering payload type `com.cloudflare.warp` → `/Library/Managed
  Preferences/com.cloudflare.warp.plist`.
- **Linux:** `/var/lib/cloudflare-warp/mdm.xml`.

### Device-wide enrollment (confirmed NOT per-user)
Set `organization` = your Zero Trust team name + `auth_client_id`/`auth_client_secret` = a **service
token**, and create a device-enrollment policy with **Action = "Service Auth."** The **device** enrolls
with no user/IdP login (such devices show email `non_identity@<team>.cloudflareaccess.com`). Trade-off:
identity-based Access policies can't be enforced on service-token-enrolled devices.

Key parameters: `organization`, `service_mode` (`warp`|`proxy`|`postureonly`|`tunnelonly`|`1dot1`),
`auto_connect` (0–1440 min), `switch_locked`, `onboarding`, `auth_client_id`, `auth_client_secret`,
`warp_tunnel_protocol` (`masque`|`wireguard`), `support_url`, `display_name`.

### How to push it in Fleet (per team = per site)

| Platform | Mechanism | Why |
|---|---|---|
| **Windows** | **Fleet script** (PowerShell, SYSTEM) that writes `C:\ProgramData\Cloudflare\mdm.xml`; or a Fleet **software package** = the MSI with `ORGANIZATION`/`AUTH_CLIENT_*` properties | Mirrors Cloudflare's own Intune guidance (Intune deploys WARP on Windows via a **PowerShell script writing mdm.xml**). A Windows MDM profile can't deliver a *file*, and WARP ignores registry/CSP for these settings |
| **macOS** | **Fleet custom config profile** — `.mobileconfig`, PayloadType `com.cloudflare.warp`, PayloadIdentifier `cloudflare_warp`, **Device** channel | Native, declarative, self-healing; matches Intune's macOS method |
| **Linux** | Fleet script writing `/var/lib/cloudflare-warp/mdm.xml` | Same file model |

Per-site scoping = place the script/profile in the **team ("Fleet")** that represents the site (GitOps
`controls`/`scripts`/`policies` per team). **`auth_client_secret` is sensitive** — deliver via Fleet's
profile/script **secret variables**, not plaintext GitOps.

> Note: Fleet *can* push arbitrary Windows CSP/SyncML (`Add`/`Replace`/`Exec`; only **BitLocker** is
> blocklisted — `server/fleet/windows_mdm.go:298-318`), so the registry-CSP capability exists — it's just
> the wrong tool for WARP specifically, since WARP uses a file, not a registry/CSP node.

### WARP status for the coverage matrix (a `HostStatusProvider` signal)
- **Installed + running** (osquery-native): Windows `services` name `CloudflareWARP` + `processes`
  `warp-svc.exe`; macOS LaunchDaemon `com.cloudflare.1dot1dot1dot1.macos.warp.daemon`; Linux service
  `warp-svc`.
- **Configured org** (macOS, osquery-native via `plist` table):
  `SELECT value FROM plist WHERE path='/Library/Managed Preferences/com.cloudflare.warp.plist' AND key='organization';`
- **Enrolled + connected + org** (all platforms, **not** osquery-native — osquery can't run the CLI or
  parse `mdm.xml`): a Fleet **script** running `warp-cli status` (look for `Status update: Connected`),
  `warp-cli account` (shows org), `warp-cli registration show`.

So WARP appears twice in the product: a **coverage-matrix status column** (installed/enrolled/connected)
and a **per-site config target** (mdm.xml/profile). Both ride patterns already in scope
([PLUGINS.md](./PLUGINS.md) `HostStatusProvider` + Fleet scripts/profiles).

### Drift verification (close the loop in Fleet)
- **macOS** policy (osquery-native): `SELECT 1 FROM plist WHERE path='/Library/Managed
  Preferences/com.cloudflare.warp.plist' AND key='organization' AND value='<your-team>';`
- **Windows** policy: check `services` (CloudflareWARP running) + `file`/`hash` of `mdm.xml`; for true
  enrollment drift use a **script-based check** (`warp-cli account`/`status`). Wire the policy's
  `run_script` automation to re-write `mdm.xml` on drift.

## Citations
- Salt masterless (`salt-call --local`, no minion daemon): docs.saltproject.io standalone_minion / quickstart
- DSC v3 (Rust CLI, no LCM/MOF, cross-platform): devblogs.microsoft.com "Announcing DSC v3" / "…v3.2.0"; learn.microsoft.com/powershell/dsc
- ansible-pull Windows caveat: docs.ansible.com windows_faq
- WARP parameters / managed deployment / Intune method / device enrollment: developers.cloudflare.com/cloudflare-one/connections/connect-devices/warp/deployment/mdm-deployment/ (+ /parameters/, /partners/intune/) and /team-and-resources/devices/warp/deployment/device-enrollment/
- Repo (verified): `server/fleet/windows_mdm.go:257-318` (only BitLocker blocklisted), `server/service/windows_mdm_profiles.go:23-77` (raw SyncML), `orbit/pkg/scripts/exec_windows.go:17` (SYSTEM), `server/service/osquery.go` (boolean policy + script automations), `pkg/spec/gitops.go` (GitOps declarables; run_script team-only)

## Flagged unknowns (verify on a real host before shipping)
1. "Intune Settings Catalog uses DSC v3" — overstated; don't assert it.
2. Exact Windows WARP `services.name` string (varies by client version) — verify with
   `SELECT name,display_name FROM services WHERE display_name LIKE '%Cloudflare%'`.
3. `warp-cli status`/`account` exact output lines (CLI command names have changed across versions) —
   capture real output for robust parsing.
4. DSC v3 native resource breadth is still limited (Registry/File/WindowsPackage prominent); services and
   richer coverage often need the PowerShell adapter (so PowerShell must be present).
