# AI-INSTALLER.md — install apps that have no silent path (honest design)

> For `.exe`/`.msi` apps with **no silent-install path**: detect a silent install where one exists, and —
> only as a gated last resort — record an admin's GUI install once and replay it. Verified against Fleet's
> orbit code + Windows platform constraints. **Bottom line: silent-install-first is the product;
> AI-GUI-replay is a threat-modeled, non-MVP, last-resort tier.** Companion to
> [RISK-REGISTER.md #6](./RISK-REGISTER.md), [OSS.md §8.1](./OSS.md#81-software--patch-management).

## The honest framing

The pitch — *"an AI watches an admin install once, then repeats it everywhere and verifies it"* — is
appealing but **over-promises**, and collides with a hard Windows wall. So we split it into two tiers and
ship the safe one:

- **Tier 1 (the product): silent-install-first + layered verification.** Fully buildable on today's MIT
  primitives, no agent changes. This is what MVP ships.
- **Tier 2 (gated, non-MVP, last resort): AI GUI record/replay.** Genuinely useful for the long tail of
  GUI-only installers, but **admin RCE by construction** and brittle — behind Tier 1, sandboxed, signed,
  human-approved. Not a headline capability.

## Tier 1 — silent-install-first (buildable now)

1. **Detect/derive a silent install.** Most `.exe`/`.msi` have silent flags (`/S`, `/qn`, MSI properties,
   winget `--silent`, choco `-y`, Ninite silent `.exe`). The software pipeline (OSS.md §8.1) already
   models these as install-scripts on a `software_installer` row; orbit runs them as SYSTEM
   (`orbit/pkg/scripts/exec_windows.go:17`).
2. **Verify in layers (corrected signals):**

   | Signal | When | Source |
   |---|---|---|
   | **Install-script exit code** + optional post-install script with **auto-rollback** on non-zero | **synchronous** — the real gate | `orbit/pkg/installer/installer.go:509-545` |
   | **Registry Uninstall/ARP key present** | synchronous post-install assertion | osquery `registry` table (`queries.go:255-269`) |
   | **Process launched** (does it start?) | synchronous-ish | osquery `processes` |
   | **Software inventory shows it** | **eventual (~hourly), NOT the gate** | osquery `programs` (= the ARP/Uninstall registry, `queries.go:1300-1304`) |

   > ⚠️ **"A window appeared" is NOT a usable signal** — osquery has no window-enumeration table. A "window
   > launched" check could only come from the GUI-automation harness itself, which is **circular** (the
   > component under test can't verify itself). Drop it. Rely on **exit code + registry key + process +
   > eventual inventory**.
3. **Report up** into the coverage matrix / an activity (installed? healthy? launches?).

This tier is real, layered, and covers the large majority of apps.

## Tier 2 — AI GUI record/replay (gated last resort)

Only for apps with genuinely no silent path. Design honestly around three hard facts:

### The Session 0 wall (a category error to avoid)
Orbit's SYSTEM PowerShell runner (`exec_windows.go:17`) runs **non-interactively in Windows Session 0** and
**cannot see or click the interactive user's desktop** — Session 0 Isolation, a Microsoft security
boundary since Vista. **"Reuse orbit's script runner to drive the wizard" is impossible.** GUI automation
must run **in the user's interactive session** via the separate run-as-logged-in-user path
(`orbit/pkg/execuser/execuser_windows.go`) — a distinct, higher-risk surface — or inside an ephemeral VM.

### Brittleness (why "watch once, repeat everywhere" isn't dependable)
GUI record/replay of arbitrary third-party wizards breaks across machines: DPI/resolution/theme, dialog
reordering, UAC/reboot prompts, localization, per-version UI drift, timing races, and pre-existing-install
branching. **Coordinate-based replay is worst; vision/computer-use replay is more robust but slow, costly,
and still fails on unseen dialogs.** Treat it as best-effort with mandatory verification, never a reliable
primitive.

### It's admin RCE by construction (threat model — RISK-REGISTER #6)
Replaying admin-privileged GUI actions **is** arbitrary code execution as admin. Controls (all required):
- **Ephemeral, locked-down VM for CAPTURE/authoring only.** ⚠️ The VM bounds *recording*, **not production
  delivery** — a VM capture yields a *recording*, not an installed app on the real endpoint; to install on
  targets you must replay on real endpoints, which reintroduces the full risk. Say this out loud.
- **Signed + pinned recordings**; refuse unsigned/modified. **No cross-tenant recording reuse** (embedded
  secrets/paths).
- **Allowlisted, verifiable action vocabulary** rather than free-form UI control.
- **On-screen text = untrusted DATA, never instruction** — a malicious installer's UI text is a real
  prompt-injection vector against a vision agent.
- **Per-step independent success check** (never trust "replay finished") + **human approval** + a
  second-approver for any fleet-wide replay. Idempotent + rollback on failure.

### The AI layer
Our stack is **Claude** (computer-use-capable models) for the watch→generalize→replay→verify agent. Keep
it architecture-level here; confirm model/tool specifics (the computer-use tool, model IDs, limits)
against the **`claude-api`** reference when we build Tier 2. The agent's role: produce a generalized,
*allowlisted* action plan from the recording, drive the interactive session handling expected dialogs, and
run the independent per-step checks — **not** free-form control of an admin desktop.

## Phasing
1. **MVP:** Tier 1 only (silent detection + layered verification + coverage/activity reporting).
2. **Later, if justified:** Tier 2 authoring in an ephemeral VM → signed recording → gated, human-approved,
   per-step-verified replay via the interactive-session agent. Decide per app whether the value justifies
   the admin-RCE risk *at all* — often the answer is "package it silently instead."

## Verified facts (receipts)
- Silent SYSTEM exec, non-interactive Session 0: `orbit/pkg/scripts/exec_windows.go:17`.
- Interactive-session path (the only GUI route): `orbit/pkg/execuser/execuser_windows.go`.
- Exit-code + post-install rollback gate: `orbit/pkg/installer/installer.go:509-545`.
- Registry/ARP + inventory signals: `server/service/osquery_utils/queries.go:255-269, :1300-1304`.
- No osquery window table (window-detection unavailable): grep of `queries.go` finds none.
- Threat posture already recorded: `RISK-REGISTER.md #6` (🔴 critical, admin RCE by construction).
