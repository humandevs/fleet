#requires -RunAsAdministrator
<#
.SYNOPSIS
  Build a self-provisioning Fleet appliance VM on Hyper-V: create the VM, unattended-install Rocky 9 via a
  kickstart on an OEMDRV ISO, then let the box provision itself (Docker Compose deps + Fleet CE build +
  community plugins) on first boot via Ansible-against-localhost.

.DESCRIPTION
  End-to-end, no interactive install steps. Steps performed:
    1. Render rocky-fleet.ks.template -> ks.cfg (hostname, admin user, SSH key, fork repo/branch).
    2. Build a tiny ISO labeled OEMDRV containing ks.cfg (Anaconda auto-loads it - no boot-param editing).
    3. Create a Gen-2 VM, attach the Rocky ISO (boot) + the OEMDRV ISO (kickstart), start it.
    4. Rocky installs unattended, reboots, and the fleet-firstboot systemd unit runs the Ansible playbook.

  Rocky has no "LTSB" (that's Windows terminology) - the Rocky 9 line is supported ~10 years (to 2032), and
  -RockyIso defaults to the 9-latest minimal ISO. Pin an exact point release for reproducibility if desired.

.EXAMPLE
  # Bring your own SSH key:
  .\Build-FleetAppliance.ps1 -SshPublicKeyPath $HOME\.ssh\id_ed25519.pub `
    -RepoUrl https://github.com/your-org/fleet.git -Branch human-dev

.EXAMPLE
  # No key yet - let the script generate one (saved under <VMPath>\<VMName>-ssh):
  .\Build-FleetAppliance.ps1 -GenerateSshKey -RepoUrl https://github.com/your-org/fleet.git

.EXAMPLE
  # No key in the image at all - password auth (user 'fleet' / -AdminPassword):
  .\Build-FleetAppliance.ps1 -AdminPassword 'S3tSomething' -RepoUrl https://github.com/your-org/fleet.git
#>
[CmdletBinding()]
param(
  [string]$VMName        = "fleet-prod",
  [string]$VMPath        = "C:\HyperV",           # where generated ISOs live (and the VHDX, unless -VhdxPath)
  [string]$VhdxPath      = "",                     # VM disk location: a full *.vhdx path OR a directory.
                                                   # Empty => <VMPath>\<VMName>.vhdx. Point at another drive here.
  [string]$RockyIso      = "C:\isos\Rocky-9-latest-x86_64-minimal.iso",
  [string]$SwitchName    = "Default Switch",
  [string]$SshPublicKeyPath,                    # optional: bring your own public key
  [switch]$GenerateSshKey,                       # optional: create (or REUSE) a keypair for the appliance
  [string]$SshKeyName    = "fleet_ceplus_ed25519_a",  # -GenerateSshKey key filename; reused across builds
                                                       # so you never get locked out. Override for a distinct key.
  [string]$AdminUser     = "fleet",
  [string]$AdminPassword = "fleet-appliance",   # console + password-SSH login. CHANGE for anything exposed.
  # Source: LOCAL by default (private repo, and captures uncommitted work) - the local tree is packaged onto
  # a FLEETSRC ISO the VM extracts on first boot, so no git access to the private repo is needed. Defaults to
  # the repo root relative to this script (human\appliance\..\..). Pass -RepoUrl instead to git-clone a
  # remote (public, or private via https://<token>@github.com/org/fleet.git).
  [string]$RepoSource,                           # local path; default = repo root (resolved below)
  [string]$RepoUrl       = "",                    # remote fallback; overrides local when set
  [string]$Branch        = "human-dev",
  [int]$Cpu              = 4,                      # logical processors (vCPUs)
  [int64]$MemoryStartup  = 4GB,                    # startup RAM
  [int64]$MemoryMin      = 2GB,                    # dynamic-memory floor
  [int64]$MemoryMax      = 8GB,                    # dynamic-memory ceiling
  [int64]$DiskSize       = 60GB
)

$ErrorActionPreference = "Stop"
$here = Split-Path -Parent $MyInvocation.MyCommand.Path

# --- Minimal IMAPI2-based ISO builder (no Windows ADK / oscdimg needed). Sets the volume label. ---
function New-DataIso {
  param([string]$SourceDir, [string]$IsoPath, [string]$VolumeLabel)
  if (-not ("IsoFileWriter" -as [type])) {
    Add-Type -TypeDefinition @"
using System;
using System.IO;
using System.Runtime.InteropServices;
using System.Runtime.InteropServices.ComTypes;
public static class IsoFileWriter {
  public static void Write(object stream, string path) {
    var i = (IStream)stream;
    var fs = File.Create(path);
    var buf = new byte[2048]; int read = 0; IntPtr pRead = Marshal.AllocHGlobal(4);
    try {
      do { i.Read(buf, buf.Length, pRead); read = Marshal.ReadInt32(pRead); if (read>0) fs.Write(buf,0,read); }
      while (read == buf.Length);
    } finally { fs.Flush(); fs.Close(); Marshal.FreeHGlobal(pRead); }
  }
}
"@
  }
  $fsi = New-Object -ComObject IMAPI2FS.MsftFileSystemImage
  $fsi.FileSystemsToCreate = 3   # ISO9660 + Joliet
  $fsi.VolumeName = $VolumeLabel
  $fsi.Root.AddTree($SourceDir, $false)
  $result = $fsi.CreateResultImage()
  [IsoFileWriter]::Write($result.ImageStream, $IsoPath)
}

# Next un-used key name, so "make a new key" never clobbers one an existing VM may still use. Rotates the
# _a/_b/_c suffix when present, else appends _2, _3, ...
function Get-NextKeyName {
  param([string]$Dir, [string]$Base)
  if ($Base -match '_([a-z])$') {
    for ($n = [int][char]$Matches[1] + 1; $n -le [int][char]'z'; $n++) {
      $cand = $Base -replace '_[a-z]$', ("_" + [char]$n)
      if (-not (Test-Path (Join-Path $Dir "$cand.pub"))) { return $cand }
    }
  }
  $i = 2
  while (Test-Path (Join-Path $Dir "$($Base)_$i.pub")) { $i++ }
  return "$($Base)_$i"
}

# --- 0. Resolve SSH access mode: key (given), key (generated), or password-only (no key in the image) ---
if ($GenerateSshKey) {
  $keyDir = Join-Path $VMPath "$VMName-ssh"
  New-Item -ItemType Directory -Force -Path $keyDir | Out-Null
  # Stable, fleet_ceplus-labeled key. If it already exists we PROMPT (default Y = reuse, so a rebuild never
  # locks you out); answering n makes a new key at the next suffix (_a -> _b), never clobbering the old one.
  # Non-interactive runs auto-reuse. Override the base name with -SshKeyName.
  $genKey = Join-Path $keyDir $SshKeyName
  $pub    = "$genKey.pub"
  if (Test-Path $pub) {
    $reuse = $true
    if ([Environment]::UserInteractive) {
      $ans = Read-Host "SSH key '$SshKeyName' already exists in $keyDir. Reuse it? [Y/n]"
      if ($ans -match '^\s*[nN]') { $reuse = $false }
    }
    if ($reuse) {
      Write-Host "==> Reusing existing SSH key: $genKey" -ForegroundColor Cyan
    } else {
      $SshKeyName = Get-NextKeyName $keyDir $SshKeyName
      $genKey = Join-Path $keyDir $SshKeyName
      $pub    = "$genKey.pub"
      Write-Host "==> Generating NEW SSH key: $genKey" -ForegroundColor Cyan
      & ssh-keygen -t ed25519 -C "fleet_ceplus-$VMName" -f $genKey -N '""' -q
      if ($LASTEXITCODE -ne 0) { throw "ssh-keygen failed. Ensure the OpenSSH client is installed (Windows 10/11 includes it)." }
    }
  } elseif (Test-Path $genKey) {
    # Private key exists but public is missing: derive the public (avoids ssh-keygen's overwrite prompt).
    & ssh-keygen -y -f $genKey | Set-Content -Encoding Ascii $pub
    Write-Host "==> Reused existing private key; regenerated public: $pub" -ForegroundColor Cyan
  } else {
    Write-Host "==> Generating SSH keypair (no passphrase) at $genKey ..."
    & ssh-keygen -t ed25519 -C "fleet_ceplus-$VMName" -f $genKey -N '""' -q
    if ($LASTEXITCODE -ne 0) { throw "ssh-keygen failed. Ensure the OpenSSH client is installed (Windows 10/11 includes it)." }
  }
  $SshPublicKeyPath = $pub
  Write-Host "    Key: $genKey  (public half embedded)" -ForegroundColor Cyan
  Write-Host "    Connect: ssh -i $genKey $AdminUser@<vm-ip>" -ForegroundColor Cyan
}

$sshLine = ""
if ($SshPublicKeyPath) {
  if (-not (Test-Path $SshPublicKeyPath)) { throw "SSH public key not found: $SshPublicKeyPath" }
  $sshKey  = (Get-Content -Raw $SshPublicKeyPath).Trim()
  $sshLine = "sshkey --username=$AdminUser `"$sshKey`""
  Write-Host "==> SSH access: key ($SshPublicKeyPath)"
} else {
  Write-Host "==> SSH access: PASSWORD only - no key baked into the image." -ForegroundColor Yellow
  Write-Host "    Log in with user '$AdminUser' / -AdminPassword (console or 'ssh $AdminUser@<vm-ip>')."
  Write-Host "    Add a key after first login and disable password auth for anything exposed." -ForegroundColor Yellow
}

# --- Source mode: LOCAL working tree (default) packaged onto a FLEETSRC ISO, or REMOTE git. ---
$srcStage = $null
if (-not $RepoUrl) {
  if (-not $RepoSource) { $RepoSource = (Resolve-Path (Join-Path $here "..\..")).Path }
  if (-not (Test-Path (Join-Path $RepoSource "go.mod"))) {
    throw "RepoSource '$RepoSource' doesn't look like the Fleet repo (no go.mod). Pass -RepoSource <path> or -RepoUrl <url>."
  }
  Write-Host "==> Source: LOCAL working tree at $RepoSource (packaged onto a FLEETSRC ISO - no repo network access needed)."
  $srcStage = Join-Path $env:TEMP "fleet-src-stage"
  Remove-Item $srcStage -Recurse -Force -ErrorAction SilentlyContinue
  New-Item -ItemType Directory -Force -Path $srcStage | Out-Null
  $tgz = Join-Path $srcStage "fleet-src.tar.gz"
  Write-Host "    Archiving working tree (excluding .git / node_modules / build; captures uncommitted work)..."
  # Windows bsdtar strips the leading './' before matching --exclude, so a top-level dir needs a BARE pattern
  # (.git), while nested ones need '*/' (*/.git for submodules). An --exclude-from file also dodges
  # PowerShell's native-argument quoting. (Confirmed against bsdtar 3.8.4.)
  $exFile = Join-Path $srcStage "excludes.txt"
  @('.git','.git/*','*/.git','*/.git/*','*node_modules*','build','build/*',
    '.cache','.cache/*','*/.cache','*/.cache/*','*.vhdx') | Set-Content -Encoding Ascii $exFile
  $tarExe = Join-Path $env:SystemRoot "System32\tar.exe"   # absolute path -> guaranteed Windows bsdtar, not a PATH tar
  & $tarExe -czf $tgz -C $RepoSource --exclude-from=$exFile .
  if ($LASTEXITCODE -ne 0) { throw "tar failed packaging the repo. Ensure $tarExe exists (Windows 10/11 includes it)." }
  Write-Host ("    Archive: {0} MB (build version metadata will be blank - no .git; fine for dev)." -f [math]::Round((Get-Item $tgz).Length/1MB))
} else {
  Write-Host "==> Source: REMOTE git ($RepoUrl @ $Branch)."
}

# --- 1. Render the kickstart from the template ---
Write-Host "==> Rendering kickstart..."
$ks = Get-Content -Raw (Join-Path $here "rocky-fleet.ks.template")
$ks = $ks.Replace("@@HOSTNAME@@",    $VMName).
          Replace("@@USERNAME@@",    $AdminUser).
          Replace("@@PASSWORD@@",    $AdminPassword).
          Replace("@@SSHKEY_LINE@@", $sshLine).
          Replace("@@REPO@@",        $RepoUrl).
          Replace("@@BRANCH@@",      $Branch)

$stage = Join-Path $env:TEMP "fleet-oemdrv"
New-Item -ItemType Directory -Force -Path $stage | Out-Null
# Anaconda looks for a file named exactly ks.cfg on the OEMDRV volume. Write LF-only (no CRLF).
[IO.File]::WriteAllText((Join-Path $stage "ks.cfg"), ($ks -replace "`r`n","`n"))

# --- 2. Build the OEMDRV kickstart ISO ---
Write-Host "==> Building OEMDRV kickstart ISO..."
New-Item -ItemType Directory -Force -Path $VMPath | Out-Null
$oemIso = Join-Path $VMPath "$VMName-oemdrv.iso"
if (Test-Path $oemIso) { Remove-Item $oemIso -Force }
New-DataIso -SourceDir $stage -IsoPath $oemIso -VolumeLabel "OEMDRV"

# FLEETSRC ISO (local mode): the working-tree archive the VM extracts on first boot.
$srcIso = $null
if ($srcStage) {
  Write-Host "==> Building FLEETSRC source ISO..."
  $srcIso = Join-Path $VMPath "$VMName-fleetsrc.iso"
  if (Test-Path $srcIso) { Remove-Item $srcIso -Force }
  New-DataIso -SourceDir $srcStage -IsoPath $srcIso -VolumeLabel "FLEETSRC"
}

# --- 3. Create + configure the VM ---
Write-Host "==> Creating VM $VMName..."
if (Get-VM -Name $VMName -ErrorAction SilentlyContinue) {
  throw "VM '$VMName' already exists. Remove it first: Stop-VM $VMName -Force; Remove-VM $VMName -Force"
}
# Resolve the VHDX path: full *.vhdx -> use as-is; a directory -> <dir>\<VMName>.vhdx; empty -> under VMPath.
if (-not $VhdxPath) {
  $vhd = Join-Path $VMPath "$VMName.vhdx"
} elseif ($VhdxPath -match '\.vhdx$') {
  $vhd = $VhdxPath
} else {
  $vhd = Join-Path $VhdxPath "$VMName.vhdx"
}
$vhdDir = Split-Path -Parent $vhd
New-Item -ItemType Directory -Force -Path $vhdDir | Out-Null
if (Test-Path $vhd) { throw "VHDX already exists: $vhd (remove it or choose another -VhdxPath)." }
if ($MemoryMin -gt $MemoryStartup -or $MemoryStartup -gt $MemoryMax) {
  throw "Memory must satisfy MemoryMin <= MemoryStartup <= MemoryMax (min=$MemoryMin startup=$MemoryStartup max=$MemoryMax)."
}
Write-Host "==> Disk: $vhd ($([math]::Round($DiskSize/1GB)) GB) | vCPU: $Cpu | RAM: $([math]::Round($MemoryStartup/1GB))GB (dyn $([math]::Round($MemoryMin/1GB))-$([math]::Round($MemoryMax/1GB))GB)"

New-VM -Name $VMName -Generation 2 -MemoryStartupBytes $MemoryStartup `
  -NewVHDPath $vhd -NewVHDSizeBytes $DiskSize -SwitchName $SwitchName | Out-Null
Set-VMProcessor $VMName -Count $Cpu
Set-VMMemory    $VMName -DynamicMemoryEnabled $true -MinimumBytes $MemoryMin -MaximumBytes $MemoryMax
Set-VM $VMName -AutomaticStartAction StartIfRunning -AutomaticStopAction Save -CheckpointType Disabled

# Rocky boot ISO + the OEMDRV kickstart ISO (+ FLEETSRC source ISO in local mode).
Add-VMDvdDrive $VMName -Path $RockyIso
Add-VMDvdDrive $VMName -Path $oemIso
if ($srcIso) { Add-VMDvdDrive $VMName -Path $srcIso }
# Boot the Rocky install DVD first.
$bootDvd = Get-VMDvdDrive $VMName | Where-Object { $_.Path -eq $RockyIso }
Set-VMFirmware $VMName -FirstBootDevice $bootDvd
# Rocky is signed under the MS UEFI CA - keep Secure Boot on with that template.
Set-VMFirmware $VMName -SecureBootTemplate MicrosoftUEFICertificateAuthority

Write-Host "==> Starting VM (unattended Rocky install begins now)..."
Start-VM $VMName

Write-Host ""
Write-Host "Appliance build kicked off. What happens next, hands-off:" -ForegroundColor Green
Write-Host "  * Rocky installs from the kickstart, then reboots and ejects the install media."
Write-Host "  * On first boot, fleet-firstboot.service runs the Ansible playbook: Docker Compose"
Write-Host "    (MySQL + Redis) + builds Fleet CE from $Branch + community plugins + systemd service."
Write-Host ""
Write-Host "Watch the console:   vmconnect.exe localhost $VMName"
Write-Host "Find the IP once up:  Get-VMNetworkAdapter -VMName $VMName | Select IPAddresses"
Write-Host "Provision log (SSH):  sudo tail -f /var/log/fleet-firstboot.log"
Write-Host "Fleet UI when done:   https://<vm-ip>:8080"
Write-Host ""
Write-Host "NOTE: after install completes you can detach the OEMDRV ISO (contains the kickstart):"
Write-Host "  Get-VMDvdDrive $VMName | Where-Object Path -eq '$oemIso' | Remove-VMDvdDrive"
