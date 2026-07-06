# Fleet golden image (Packer)

Bake a reusable Hyper-V image so new Fleet instances (DR spare, per-client boxes) boot ready in **~1 min**
instead of the ~15-min from-scratch build. The expensive work (Docker, Go/Node, the Fleet build, image
pulls) is baked once; each clone personalizes only the cheap per-instance bits on first boot.

```
packer build fleet-golden.pkr.hcl
  ├─ boot Rocky ISO + ks.cfg (OEMDRV)         → minimal OS + `packer` build user
  ├─ scripts/provision.sh                      → run our Ansible playbook: Docker + build Fleet (+ bake smoke)
  └─ scripts/generalize.sh                     → strip DB volume + private key + machine-id + host keys;
                                                  install fleet-personalize.service; remove packer user
  → output-fleet-golden/*.vhdx  (the golden image)

clone the VHDX → boot →
  fleet-personalize.service (once): unique private key → fresh DB → prepare db → start Fleet → smoke → done
```

## Build

```powershell
cd human\appliance\packer
packer init .
packer build -var repo_url=https://github.com/your-org/fleet.git .
# private fork: -var repo_url=https://<PAT>@github.com/your-org/fleet.git
# pin the ISO/checksum for reproducibility: -var iso_url=C:\isos\Rocky-9.6-...iso -var iso_checksum=file:...CHECKSUM
```

Prereqs: [Packer](https://developer.hashicorp.com/packer) + the Hyper-V plugin (installed by `packer init`),
elevated PowerShell, Hyper-V enabled.

## Stamp a VM from the image

```powershell
$img = "C:\HyperV\golden\fleet-golden.vhdx"     # copy the Packer output here
Copy-Item .\output-fleet-golden\*.vhdx $img
New-VM -Name fleet-clientA -Generation 2 -MemoryStartupBytes 4GB -VHDPath $img -SwitchName "fleet-ext"
Set-VMFirmware fleet-clientA -SecureBootTemplate MicrosoftUEFICertificateAuthority
Start-VM fleet-clientA
# first boot runs fleet-personalize → https://<ip>:8080 in ~1 min
```

(Copy the VHDX per clone — don't point two VMs at one disk. For many clones, use a differencing disk off the
golden VHDX.)

## Which path do I use?

| | One-off appliance ([../README.md](../README.md)) | Golden image (here) |
|---|---|---|
| Build tool | `Build-FleetAppliance.ps1` | `packer build` |
| Provision | first boot, from scratch (~15 min) | baked; clone personalizes (~1 min) |
| Use for | the first box, quick tests | the DR spare + many per-client instances |

Both share the **same Ansible roles** — the golden image just moves the work from first-boot to bake-time.

## Status / caveats

Untested here (no Hyper-V/Packer in this environment) — a working skeleton to finish on the VM. Likely
tuning points: `iso_checksum` (set a real one), the `boot_command` menu nudge (depends on your ISO's menu),
and confirming `docker compose down -v` + `fleet prepare db` sequencing on a real clone. The bake-time and
personalize smoke checks (`/healthz`) will surface a broken build rather than shipping a dead image.
