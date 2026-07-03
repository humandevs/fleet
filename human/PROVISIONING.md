# PROVISIONING.md — config-as-code machine rollout (PDQ Deploy replacement) + per-site cloud config

> How a new/managed machine converges to its client/site's declared baseline — software + config +
> cloud-storage — as code. Replaces PDQ Deploy for remote rollout. Builds on
> [CONFIG-MGMT.md](./CONFIG-MGMT.md) (the convergence loop) and [OSS.md §8.1](./OSS.md#81-software--patch-management)
> (the software pipeline). KFM mechanics verified against Microsoft/Google docs; **two capability gaps in
> the stated goal are flagged below — read them.**

## 1. The model: a per-site baseline, GitOps-declared, applied on enrollment

"Replace PDQ Deploy" = declare each client/site's **baseline** as code and converge every machine to it:

```
Site baseline (GitOps, per Team/"Fleet"):
  software:   winget/choco/Ninite install-scripts + custom installers   (OSS.md §8.1 pipeline)
  config:     config profiles (registry/CSP) + orbit scripts + DSC v3     (CONFIG-MGMT.md)
  cloud:      OneDrive KFM / Google Drive per cloud_platform (§3-4 below)
  policies:   osquery checks that verify the baseline converged (drift → re-apply)
        │
   applied on enrollment ──────────────────────────────────────────────┐
        │                                                               │
  Autopilot path: Windows Autopilot MDM-enrolls → our baseline applies  │  ← both converge to
  Our-installer path: our agent/installer enrolls → our baseline applies┘     the same baseline
```

The convergence loop (CONFIG-MGMT.md) is the engine: **GitOps declares → profiles/scripts/DSC apply →
policies verify → automations re-apply on drift.** Provisioning is just "apply the site baseline at
enrollment time and keep it converged." No new engine — a new *object* (the site baseline) + the
enrollment trigger.

- **Autopilot handoff:** Autopilot's job is only to MDM-enroll the device (into our MDM or Intune→ours);
  our platform then applies the baseline. The alternative "our platform of installers" path enrolls via
  our agent and applies the same baseline. *(Verify the exact Intune→our-MDM handoff if clients keep
  Intune as the MDM authority — that's a co-management question.)*
- **Effort:** the software pipeline is the OSS.md §6 #2 rebuild (XL, already scoped); the "site baseline"
  object + enrollment-trigger convergence is **M** on top of it; macOS first-boot is the ee `setup_experience`
  (rebuild MIT). Reuses GitOps team spec (`pkg/spec/gitops.go`), config profiles, scripts, policies.

## 2. The `cloud_platform` client/site field

Most clients are **either Google or Microsoft**, so add a per-site field that drives cloud config (and
later SSO, etc.):

- **Field:** `cloud_platform: microsoft | google | none` on the Team/site config — lives on the Team model
  + the GitOps team spec (rides the Teams rebuild, OSS.md §6 #1). For the internal/pre-Teams phase it can
  be a team custom setting / GitOps value.
- **Selection:** the baseline's cloud-storage profile/script set is chosen by this field — a
  `microsoft` site gets the OneDrive profiles (§3), a `google` site gets the Google Drive config (§4).
  Same team-scoped profile/script mechanism as the WARP `mdm.xml` example in
  [CONFIG-MGMT.md](./CONFIG-MGMT.md#worked-example-warp-config-per-clientsite).

## 3. Microsoft — OneDrive Known Folder Move (verified)

Push these via a team-scoped Windows **config profile (registry CSP)** or orbit script, under
`HKLM\SOFTWARE\Policies\Microsoft\OneDrive`:

| Value | Type | Effect |
|---|---|---|
| `KFMSilentOptIn` | `"<Entra tenant GUID>"` | Silently redirect known folders to OneDrive. **Value is the tenant ID as a GUID (8-4-4-4-12)** — injected per-site. **No-ops if the GUID ≠ the tenant of the account signed into OneDrive on the device.** |
| `KFMSilentOptInDesktop` / `…Documents` / `…Pictures` | `dword:1` | Per-folder control. **If none are set, KFM moves all three** (Desktop+Documents+Pictures). |
| `KFMSilentOptInWithNotification` | `dword:1` | Show a toast after redirection. |
| `SilentAccountConfig` | `dword:1` | **Silently sign the user in** with their Windows/Entra credentials (Entra-joined machines) — this is the "prompt/sign-in on open" done silently. |
| `FilesOnDemandEnabled` | `dword:1` | Files On-Demand (also sets the `CldFlt` filter-driver service to auto-start). |

> ⚠️ **Gap #1 — Videos (and Music) are NOT covered by OneDrive KFM.** Microsoft, verbatim: *"The OneDrive
> Group Policy objects don't affect the Music and Videos folders."* KFM = **Desktop + Documents + Pictures
> only** (Screenshots/Camera Roll ride inside Pictures). To also protect **Videos**, you'd deploy **legacy
> Windows Folder Redirection** (a separate GPO/registry mechanism we'd have to add) — it is *not*
> reachable via OneDrive KFM. The "Documents/Desktop/Pictures/**Videos**" goal needs this extra piece on
> Windows, or drop Videos.

## 4. Google — Google Drive for desktop (verified, and more limited)

Google's model is fundamentally different — **there is no KFM-equivalent silent folder enforcement.**

- Google Workspace admins **can**: toggle capabilities org-wide (allow "mirror My Drive," "sync local
  folders to Drive," "back up external media"), and **restrict/auto sign-in to the org domain** (the
  sign-in-enforcement layer exists). Push the client-side registry policies under
  `HKLM\SOFTWARE\Google\DriveFS` (`AutoStartOnLogin`, `DisableOnboardingDialog`, domain restriction).
- Google admins **cannot**: enforce *which* folders get backed up. **Folder selection is user-driven** —
  the end user picks Desktop/Documents/etc. in the client. There is no admin key that silently redirects
  them.

> ⚠️ **Gap #2 — on Google, we can gate the capability + auto-sign-in + guide the user, but we cannot
> silently enforce Desktop/Documents/Pictures/Videos backup the way OneDrive KFM does.** Design the Google
> path as: enable the capability, restrict/auto sign-in to the client's domain, and **prompt/guide the
> user** to enable folder backup (e.g. a first-run notification + a policy that *checks* whether backup is
> on and nudges if not) — not silent enforcement.

## 5. Net on the stated goal (honest)
"Auto-sync Documents/Desktop/Pictures/Videos on both Google and Microsoft, prompt sign-in on open":
- **Microsoft:** Desktop/Documents/Pictures **silently enforced** (KFM) + silent sign-in (SilentAccountConfig).
  **Videos** needs a separate Folder-Redirection add-on.
- **Google:** capability + auto-sign-in **enforceable**; **folder selection is user-driven** (guide/nudge,
  can't force). A drift **policy** can detect "backup not configured" and alert/prompt.

So the feature ships as: **silent + complete on Microsoft (minus Videos); capability-gated + user-guided on
Google.** Set expectations with clients accordingly; don't promise silent Google folder enforcement.

## Product wiring (all config-as-code, per-site)
- `cloud_platform` field on Team/site config + GitOps team spec.
- OneDrive: team-scoped Windows registry config profile (or orbit script) with the §3 values, tenant GUID
  injected per-site (**secret/tenant-id from the encrypted per-tenant store**, per RISK-REGISTER #3).
- Google: DriveFS registry policies + a first-run guide + a "backup configured?" drift policy.
- Verification: policies that confirm KFM redirection (registry/known-folder path) and OneDrive sign-in;
  for Google, a policy that checks DriveFS running + signed-in.

## Open items
- Autopilot ↔ our-MDM handoff if clients keep Intune as MDM authority (co-management).
- Whether to build the legacy Folder-Redirection add-on for Videos on Windows.
- macOS cloud-storage equivalents if clients have Macs (iCloud/Google Drive on mac).
