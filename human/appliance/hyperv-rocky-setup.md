# Create a Rocky 9 VM in Hyper-V (manual reference)

> **For the automated appliance, use [appliance/README.md](./README.md)** — one command builds the
> VM, unattended-installs Rocky, and self-provisions Fleet. This page is the **manual, step-by-step
> equivalent** for understanding what the script does or building a box by hand.

Run the PowerShell in an **elevated** prompt on the Hyper-V host. This builds a Gen-2 Rocky 9 VM that will
run Docker Compose (MySQL + Redis) and the native Fleet binary. After it's up, provision with
[ansible/README.md](./ansible/README.md).

> **Which Rocky?** Rocky has **no "LTSB"** (that's Windows terminology). Each major version is ~10-year
> supported — Rocky **9 → 2032**. `Rocky-9-latest` = newest point release *on the supported 9 line*, so it
> IS the long-term branch; there's no separate LTSB ISO to choose. Pin `Rocky-9.6-...` for reproducibility.

## 0. Prereqs (once)

```powershell
# Enable Hyper-V (reboots if it wasn't already on):
Enable-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V -All
```

Download the **Rocky 9 minimal ISO** to e.g. `C:\isos\`:
<https://download.rockylinux.org/pub/rocky/9/isos/x86_64/Rocky-9-latest-x86_64-minimal.iso>

## 1. Variables

```powershell
$VMName     = "fleet-prod"
$VMPath     = "C:\HyperV"
$ISOPath    = "C:\isos\Rocky-9-latest-x86_64-minimal.iso"
$SwitchName = "fleet-ext"
```

## 2. Virtual switch

An **External** switch gives the VM a real LAN IP (stable, reachable from other machines and from managed
hosts/agents later) — better than the Default Switch, whose NAT IP changes on reboot.

```powershell
Get-NetAdapter | Where-Object Status -eq 'Up'          # find your physical NIC name
New-VMSwitch -Name $SwitchName -NetAdapterName "Ethernet" -AllowManagementOS $true
# Quick NAT-only alternative (no LAN IP): skip this and use -SwitchName "Default Switch" in step 3.
```

## 3. Create the VM (Gen 2)

```powershell
New-VM -Name $VMName -Generation 2 -MemoryStartupBytes 4GB `
  -NewVHDPath "$VMPath\$VMName.vhdx" -NewVHDSizeBytes 60GB -SwitchName $SwitchName
Set-VMProcessor $VMName -Count 4
Set-VMMemory    $VMName -DynamicMemoryEnabled $true -MinimumBytes 2GB -MaximumBytes 8GB

# Attach the ISO and boot it first:
Add-VMDvdDrive  $VMName -Path $ISOPath
Set-VMFirmware  $VMName -FirstBootDevice (Get-VMDvdDrive $VMName)

# Secure Boot: Rocky is signed under the MS UEFI CA, so keep it ON with that template:
Set-VMFirmware  $VMName -SecureBootTemplate MicrosoftUEFICertificateAuthority
#   (If it fails to boot, disable instead:  Set-VMFirmware $VMName -EnableSecureBoot Off)

# Save state on host shutdown; no automatic checkpoints (they interfere with the DB volume):
Set-VM $VMName -AutomaticStartAction StartIfRunning -AutomaticStopAction Save -CheckpointType Disabled

Start-VM $VMName
vmconnect.exe localhost $VMName        # opens the console window
```

## 4. Install Rocky (in the console — Anaconda installer)

Pick **Install Rocky Linux 9** at the boot menu, then set:

- **Installation Destination** → select the 60 GB disk → **Done** (default auto-partitioning is fine).
- **Network & Host Name** → toggle the adapter **ON**, set hostname `fleet-prod` → **Done**. Note the IPv4
  it gets — that's your Ansible target.
- **Software Selection** → **Minimal Install** (Docker + toolchain come from Ansible).
- **Root Password** → set one (or leave locked).
- **User Creation** → create your admin user and **check "Make this user administrator"** (adds it to
  `wheel` for sudo). This is the `ansible_user`.
- **Begin Installation** → wait → **Reboot**.

Then stop the VM from booting the installer again — remove the ISO:

```powershell
Set-VMDvdDrive $VMName -Path $null
```

## 5. First contact

Rocky minimal ships `sshd` enabled and allows SSH through the firewall by default, so from your Ansible
controller:

```bash
ssh-copy-id youruser@<vm-ip>       # push your key
ssh youruser@<vm-ip> "sudo dnf -y update && echo ok"   # sanity check + patch
```

Now provision it: [ansible/README.md](./ansible/README.md) §3–§4.

## Unattended / appliance build

Steps 3–5 above are fully automated by [appliance/Build-FleetAppliance.ps1](./Build-FleetAppliance.ps1):
it renders `appliance/rocky-fleet.ks.template` into a kickstart, builds an **OEMDRV**-labeled ISO that
Anaconda auto-loads (no boot-param editing), creates + boots the VM, and the box self-provisions Fleet on
first boot. Use that for the DR spare and future per-client VMs. See
[appliance/README.md](./README.md).
