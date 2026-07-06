# Fleet appliance — one-command build

## TL;DR

```powershell
# 1. Download the Rocky 9 ISO (elevated PowerShell on the Hyper-V host):
mkdir C:\isos -Force
curl.exe -L -o C:\isos\Rocky-9-latest-x86_64-minimal.iso `
  https://download.rockylinux.org/pub/rocky/9/isos/x86_64/Rocky-9-latest-x86_64-minimal.iso

# 2. Build the appliance VM from your LOCAL working tree (no SSH key, no repo token needed).
#    A random admin password is generated and printed (or pass -AdminPassword to set your own):
cd human\appliance
.\Build-FleetAppliance.ps1 -GenerateSshKey

# 3. Find the IP + wait for Fleet, then open the UI (watcher finds the IP even in NAT mode):
.\Watch-FleetVM.ps1 -KeyPath C:\HyperV\fleet-prod-ssh\fleet_ceplus_ed25519_a
```

First boot self-provisions in ~15 min (Docker deps + builds Fleet). For many VMs, bake a golden image once
([packer/](./packer/README.md)) so clones boot ready in ~1 min. Details below.

---

Build a **self-provisioning** Fleet VM: one PowerShell command creates the Hyper-V VM, unattended-installs
Rocky 9, and the box provisions itself on first boot (Docker Compose MySQL + Redis, builds Fleet CE from our
fork, community plugins, native systemd service). No interactive install, no manual Ansible run.

```
Build-FleetAppliance.ps1
  ├─ package the LOCAL working tree → FLEETSRC ISO   (default; private repo + uncommitted work, no token)
  ├─ render rocky-fleet.ks.template → ks.cfg         (hostname, admin user, SSH key)
  ├─ build OEMDRV ISO with ks.cfg                     (Anaconda auto-loads it — no boot-param editing)
  ├─ create Gen-2 VM + attach Rocky ISO + OEMDRV ISO + FLEETSRC ISO + start
  └─ (in the VM) Rocky installs unattended → reboots
       └─ fleet-firstboot.service: extract FLEETSRC → /opt/fleet-src, then
          ansible-playbook -i inventory/localhost.ini site.yml
            → Docker Compose deps + build Fleet + plugins + systemd service → /healthz smoke check
```

**Source is local by default** — the script packages your working tree (relative to `human\appliance`, i.e.
the repo root) onto a `FLEETSRC` ISO the VM extracts on first boot. No git access to the private repo, and
it captures **uncommitted** changes. Pass `-RepoUrl https://<token>@github.com/...` to git-clone a remote
instead; `-RepoSource <path>` to point at a different local checkout.

## Run it (elevated PowerShell on the Hyper-V host)

Pick one of the three SSH-access modes below; only `-RepoUrl` is required.

```powershell
cd human\appliance
.\Build-FleetAppliance.ps1 -RepoUrl https://github.com/your-org/fleet.git -Branch human-dev `
  <one of the SSH options below>
```

Common overrides:
- **Disk location:** `-VhdxPath D:\VMs\fleet-prod.vhdx` (a full `*.vhdx` path or a directory — put the disk
  on another drive). Defaults to `<VMPath>\<VMName>.vhdx`.
- **CPU / memory:** `-Cpu 6`, `-MemoryStartup 8GB`, `-MemoryMin 2GB`, `-MemoryMax 12GB` (dynamic memory).
- **Disk size:** `-DiskSize 80GB`. Other: `-VMName`, `-RockyIso`, `-SwitchName "fleet-ext"`,
  `-AdminUser fleet`, `-AdminPassword <pw>`.

### SSH access — you don't need a key already

The `fleet` admin user always gets a password (console + password SSH). A key is embedded **only** if you
supply one. Three ways:

1. **Have a key** → embed it: `-SshPublicKeyPath $HOME\.ssh\id_ed25519.pub`
2. **No key yet, want one** → `-GenerateSshKey`. First run creates a keypair named `fleet_ceplus_ed25519_a`
   under `<VMPath>\<VMName>-ssh\`; only the public half is embedded in the VM. On later runs, if that key
   exists the script **prompts `Reuse it? [Y/n]`** — Enter/Y reuses it (so a rebuild never locks you out),
   `n` makes a fresh key at the next suffix (`_a`→`_b`, never clobbering the old one). Non-interactive runs
   auto-reuse. It prints the key path and the exact `ssh -i <path> fleet@<vm-ip>` line. Override the base
   name with `-SshKeyName <name>`. (Or generate manually: `ssh-keygen -t ed25519 -f $HOME\.ssh\id_ed25519`,
   then use option 1.)
3. **No key in the image at all** → omit both and log in by **password**: `-AdminPassword 'SetAStrongOne'`,
   then `ssh fleet@<vm-ip>` (or the Hyper-V console). Fine for an internal/lab box; for anything exposed,
   add a key after first login and set `PasswordAuthentication no` in `/etc/ssh/sshd_config`.

> Windows 10/11 ship the OpenSSH client (`ssh`, `ssh-keygen`) for options 1–2. If missing:
> `Add-WindowsCapability -Online -Name OpenSSH.Client~~~~0.0.1.0`.

- **Private fork?** Pass a token in the URL: `-RepoUrl https://<PAT>@github.com/your-org/fleet.git` (the
  firstboot clone needs read access). Use a read-only deploy token; it lands in the VM's provision script.
- **Rocky ISO:** defaults to `C:\isos\Rocky-9-latest-x86_64-minimal.iso`. Rocky has **no "LTSB"** — the
  Rocky 9 line is ~10-year supported (to 2032); `9-latest` tracks the newest point release on that line.
  Pin an exact release (e.g. `Rocky-9.6-x86_64-minimal.iso`) for byte-reproducible builds.

## Watch / verify

```powershell
vmconnect.exe localhost fleet-prod                                  # console
Get-VMNetworkAdapter -VMName fleet-prod | Select-Object IPAddresses # the IP once networked
```
```bash
ssh fleet@<vm-ip> "sudo tail -f /var/log/fleet-firstboot.log"       # provisioning progress
# when done: https://<vm-ip>:8080   (then human/verify: npm run smoke against it)
```

**Timing:** the VM comes up in a couple of minutes; first-boot provisioning then takes ~10–20 min (it does
a full `yarn install` + webpack + `go build` of the fork). It's a one-time bake — subsequent
`ansible-playbook` runs (or a future golden image) are fast.

## How it's a true appliance

- **No interactive steps** — the kickstart (`rocky-fleet.ks.template`) drives disk, user, SSH key, packages;
  Anaconda auto-loads it from the OEMDRV-labeled ISO the script builds (via IMAPI2 — no Windows ADK needed).
- **Self-contained provisioning** — `fleet-firstboot.service` runs the same Ansible playbook we use for
  push provisioning, but with `inventory/localhost.ini` (`connection: local`). Same roles, same result.
- **Unique per appliance** — first boot generates this box's own `fleet_server_private_key` (encrypts
  secrets at rest); no shared key baked into an image.
- **Idempotent + repeatable** — re-runnable for the DR spare or future per-client instances; the natural
  next step for many instances is baking a golden image (Packer, same playbook) so provisioning is instant.

## Files

| File | Role |
|---|---|
| `Build-FleetAppliance.ps1` | Host-side orchestrator: kickstart render, OEMDRV ISO, VM create/start |
| `rocky-fleet.ks.template` | Unattended Rocky install + first-boot self-provision service |
| `./ansible/inventory/localhost.ini` | Local-connection inventory the appliance provisions against |
| `./ansible/` | The playbook/roles (Docker deps + native Fleet) — shared with push provisioning |

For the manual, step-by-step equivalent (understand what the script automates), see
[./hyperv-rocky-setup.md](./hyperv-rocky-setup.md).
