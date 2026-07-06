<#
.SYNOPSIS
  Tear down a Fleet appliance VM so you can start fresh. By DEFAULT it removes the VM + the generated
  OEMDRV/FLEETSRC ISOs but KEEPS the VHDX (deleting a disk is irreversible). Behind a typed confirmation,
  then validates the removable targets are gone.

.DESCRIPTION
  Safety posture:
   * The VHDX is KEPT by default -- it may hold a built Fleet + data, and deleting it can't be undone. Pass
     -RemoveDisk to delete it, which requires an extra 'PERMANENTLY DELETE' confirmation.
   * Only ever targets THIS VM's disk + the ISOs WE generate (<VMName>-oemdrv.iso / -fleetsrc.iso). Never the
     Rocky install ISO or any other attached media.
   * Requires typing the VM name to proceed (-Force skips it). SSH keys are kept unless -RemoveKey.
   * After deleting, re-checks every target and reports CLEAN / what remains.

  Mirrors Build-FleetAppliance.ps1's -VhdxPath / -VMPath resolution so it finds the same files.

.EXAMPLE
  .\Reset-FleetVM.ps1 -VhdxPath C:\isos\                 # remove VM + ISOs, KEEP the disk
.EXAMPLE
  .\Reset-FleetVM.ps1 -VhdxPath C:\isos\ -RemoveDisk     # also PERMANENTLY delete the disk (double-gated)
#>
[CmdletBinding()]
param(
  [string]$VMName   = "fleet-prod",
  [string]$VMPath   = "C:\HyperV",
  [string]$VhdxPath = "",
  [switch]$RemoveDisk,     # PERMANENTLY delete the VHDX too (irreversible); off by default
  [switch]$RemoveKey,      # also delete the SSH key dir (keys are normally reused)
  [switch]$PruneArchives,  # delete the archived <VMName>-old-N.vhdx disks (reclaim space); standalone or from Build
  [switch]$Force           # skip the typed confirmation(s)
)
$ErrorActionPreference = "Stop"

if (-not $VhdxPath)                 { $expectedVhdx = Join-Path $VMPath "$VMName.vhdx" }
elseif ($VhdxPath -match '\.vhdx$') { $expectedVhdx = $VhdxPath }
else                                { $expectedVhdx = Join-Path $VhdxPath "$VMName.vhdx" }

function Remove-FileHard {
  param([string]$Path)
  for ($i = 0; $i -lt 6 -and (Test-Path $Path); $i++) {
    try { Remove-Item $Path -Force -ErrorAction Stop } catch { Start-Sleep -Milliseconds 500 }
  }
  return (-not (Test-Path $Path))
}

# --- Prune archived disks (<VMName>-old-N.vhdx). Standalone cleanup, or invoked by Build to reclaim space. ---
if ($PruneArchives) {
  $vdir = Split-Path -Parent $expectedVhdx
  $base = [IO.Path]::GetFileNameWithoutExtension($expectedVhdx)
  $archives = @(Get-ChildItem -LiteralPath $vdir -Filter "$base-old-*.vhdx" -ErrorAction SilentlyContinue)
  if ($archives.Count -eq 0) { Write-Host "No archived disks ($base-old-*.vhdx) to prune in $vdir." -ForegroundColor Green; return }
  $gb = [math]::Round(($archives | Measure-Object Length -Sum).Sum / 1GB, 1)
  Write-Host ""
  Write-Host ("Prune {0} archived disk(s) using ~{1} GB in {2} (IRREVERSIBLE):" -f $archives.Count, $gb, $vdir) -ForegroundColor Yellow
  $archives | ForEach-Object { Write-Host ("  DELETE : {0}" -f $_.FullName) -ForegroundColor Red }
  if (-not $Force) {
    $t = Read-Host "Type exactly: PERMANENTLY DELETE"
    if ($t -cne 'PERMANENTLY DELETE') { Write-Host "Aborted - nothing deleted." -ForegroundColor Cyan; return }
  }
  foreach ($a in $archives) { Write-Host "Deleting $($a.FullName)"; if (-not (Remove-FileHard $a.FullName)) { Write-Host "  ! could not delete $($a.FullName)" -ForegroundColor Red } }
  $left = @(Get-ChildItem -LiteralPath $vdir -Filter "$base-old-*.vhdx" -ErrorAction SilentlyContinue)
  if ($left.Count -eq 0) { Write-Host "CLEAN - archives pruned." -ForegroundColor Green }
  else { Write-Host "Some archives remain (locked/in use?)." -ForegroundColor Red }
  return
}

# --- Discover ---
$vm = Get-VM -Name $VMName -ErrorAction SilentlyContinue

$vhdxList = @()
if ($vm) { $vhdxList += (Get-VMHardDiskDrive -VMName $VMName -ErrorAction SilentlyContinue).Path }
$vhdxList += $expectedVhdx
$vhdxList = $vhdxList | Where-Object { $_ -and (Test-Path $_) } | Select-Object -Unique

$isoList = @()
foreach ($n in @("$VMName-oemdrv.iso", "$VMName-fleetsrc.iso")) {
  $p = Join-Path $VMPath $n
  if (Test-Path $p) { $isoList += $p }
}
$keyDir = Join-Path $VMPath "$VMName-ssh"

# --- Anything to remove? (disk only counts as a target when -RemoveDisk) ---
$willRemoveDisk = $RemoveDisk -and $vhdxList.Count -gt 0
$willRemoveKey  = $RemoveKey  -and (Test-Path $keyDir)
if (-not $vm -and $isoList.Count -eq 0 -and -not $willRemoveDisk -and -not $willRemoveKey) {
  if ($vhdxList.Count -gt 0) { Write-Host "Nothing to remove for '$VMName' (the VHDX is kept). Ready to start fresh." -ForegroundColor Green }
  else { Write-Host "Nothing to remove for '$VMName' - already clean. Ready to start fresh." -ForegroundColor Green }
  return
}

# --- Plan ---
Write-Host ""
Write-Host "Planned actions for '$VMName':" -ForegroundColor Yellow
if ($vm)                 { Write-Host ("  DELETE VM   : {0} (state: {1})" -f $VMName, $vm.State) }
foreach ($i in $isoList) { Write-Host ("  DELETE ISO  : {0}" -f $i) }
foreach ($v in $vhdxList) {
  if ($willRemoveDisk) { Write-Host ("  DELETE DISK : {0}   (IRREVERSIBLE)" -f $v) -ForegroundColor Red }
  else                 { Write-Host ("  KEEP   DISK : {0}   [use -RemoveDisk to delete]" -f $v) -ForegroundColor DarkGray }
}
if ($willRemoveKey)          { Write-Host ("  DELETE KEYS : {0}" -f $keyDir) -ForegroundColor Red }
elseif (Test-Path $keyDir)   { Write-Host ("  KEEP   KEYS : {0}   [use -RemoveKey to delete]" -f $keyDir) -ForegroundColor DarkGray }
Write-Host ""

# --- Gate(s) ---
if (-not $Force) {
  $typed = Read-Host "Type the VM name '$VMName' to confirm"
  if ($typed -ne $VMName) { Write-Host "Aborted - nothing was deleted." -ForegroundColor Cyan; return }
  if ($willRemoveDisk) {
    $typed2 = Read-Host "You are about to PERMANENTLY DELETE the disk (irreversible). Type exactly: PERMANENTLY DELETE"
    if ($typed2 -cne 'PERMANENTLY DELETE') {
      Write-Host "Disk NOT deleted (kept). Proceeding with VM + ISOs only." -ForegroundColor Cyan
      $willRemoveDisk = $false
    }
  }
}

# --- Delete ---
if ($vm) {
  if ($vm.State -ne 'Off') { Write-Host "Stopping $VMName..."; Stop-VM -Name $VMName -Force -TurnOff }
  Write-Host "Removing VM $VMName..."; Remove-VM -Name $VMName -Force
}
foreach ($i in $isoList) { Write-Host "Deleting $i"; if (-not (Remove-FileHard $i)) { Write-Host "  ! could not delete $i" -ForegroundColor Red } }
if ($willRemoveDisk) {
  foreach ($v in $vhdxList) { Write-Host "PERMANENTLY deleting $v" -ForegroundColor Red; if (-not (Remove-FileHard $v)) { Write-Host "  ! could not delete $v" -ForegroundColor Red } }
}
if ($willRemoveKey) { Write-Host "Deleting keys $keyDir"; Remove-Item $keyDir -Recurse -Force -ErrorAction SilentlyContinue }

# --- Validate (only the things we intended to remove) ---
$remaining = @()
if (Get-VM -Name $VMName -ErrorAction SilentlyContinue) { $remaining += "VM $VMName" }
foreach ($i in $isoList)  { if (Test-Path $i) { $remaining += $i } }
if ($willRemoveDisk) { foreach ($v in $vhdxList) { if (Test-Path $v) { $remaining += $v } } }

Write-Host ""
if ($remaining.Count -eq 0) {
  Write-Host "CLEAN - removable targets gone. Ready to start fresh:" -ForegroundColor Green
  if (-not $willRemoveDisk -and $vhdxList.Count -gt 0) {
    Write-Host ("  (existing disk kept: {0} - Build will archive it to -old-N and take the canonical name)" -f $vhdxList[0]) -ForegroundColor DarkGray
  }
  Write-Host "  .\Build-FleetAppliance.ps1 -GenerateSshKey$(if($VhdxPath){" -VhdxPath $VhdxPath"})"
} else {
  Write-Host "NOT fully clean - these still exist (locked? in use?):" -ForegroundColor Red
  $remaining | ForEach-Object { Write-Host "  $_" }
  Write-Host "Close anything using them (vmconnect, Explorer, antivirus) and re-run."
}
