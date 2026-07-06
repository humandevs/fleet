# Fleet appliance — one-command build

## TL;DR

```powershell
# 1. Download the Rocky 9 ISO (elevated PowerShell on the Hyper-V host):
mkdir C:\isos -Force
curl.exe -L -o C:\isos\Rocky-9-latest-x86_64-minimal.iso `
  https://download.rockylinux.org/pub/rocky/9/isos/x86_64/Rocky-9-latest-x86_64-minimal.iso

# 2. Build the appliance VM (no SSH key needed — uses password auth):
cd human\appliance
.\Build-FleetAppliance.ps1 -RepoUrl https://github.com/your-org/fleet.git -AdminPassword 'SetAStrongOne'

# 3. Watch it come up, then open the UI:
vmconnect.exe localhost fleet-prod
Get-VMNetworkAdapter -VMName fleet-prod | Select-Object IPAddresses   # → https://<ip>:8080
```

First boot self-provisions in ~15 min (Docker deps + builds Fleet). For many VMs, bake a golden image once
([packer/](./packer/README.md)) so clones boot ready in ~1 min. Details below.

---

Build a **self-provisioning** Fleet VM: one PowerShell command creates the Hyper-V VM, unattended-installs
Rocky 9, and the box provisions itself on first boot (Docker Compose MySQL + Redis, builds Fleet CE from our
fork, community plugins, native systemd service). No interactive install, no manual Ansible run.

```
Build-FleetAppliance.ps1
  ├─ render rocky-fleet.ks.template → ks.cfg        (hostname, admin user, SSH key, fork repo/branch)
  ├─ build OEMDRV ISO with ks.cfg                    (Anaconda auto-loads it — no boot-param editing)
  ├─ create Gen-2 VM + attach Rocky ISO + OEMDRV ISO + start
  └─ (in the VM) Rocky installs unattended → reboots
       └─ fleet-firstboot.service runs: ansible-playbook -i inventory/localhost.ini site.yml
            → Docker Compose deps + build Fleet + plugins + systemd service
```

## Run it (elevated PowerShell on the Hyper-V host)

Pick one of the three SSH-access modes below; only `-RepoUrl` is required.

```powershell
cd human\appliance
.\Build-FleetAppliance.ps1 -RepoUrl https://github.com/your-org/fleet.git -Branch human-dev `
  <one of the SSH options below>
```

Common overrides: `-VMName`, `-RockyIso`, `-SwitchName "fleet-ext"`, `-Cpu 4`, `-DiskSize 80GB`,
`-AdminUser fleet`, `-AdminPassword <pw>`.

### SSH access — you don't need a key already

The `fleet` admin user always gets a password (console + password SSH). A key is embedded **only** if you
supply one. Three ways:

1. **Have a key** → embed it: `-SshPublicKeyPath $HOME\.ssh\id_ed25519.pub`
2. **No key yet, want one** → `-GenerateSshKey` (script runs `ssh-keygen`, saves the pair to
   `<VMPath>\<VMName>-ssh\`; only the public half is embedded). Connect later with
   `ssh -i C:\HyperV\fleet-prod-ssh\id_ed25519 fleet@<vm-ip>`. (Or generate manually:
   `ssh-keygen -t ed25519 -f $HOME\.ssh\id_ed25519`, then use option 1.)
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
