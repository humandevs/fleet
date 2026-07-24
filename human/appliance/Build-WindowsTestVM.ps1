<#
.SYNOPSIS
  Create a Windows Server 2022 Hyper-V test endpoint (a managed device to enroll fleetd + ScreenConnect
  onto). Gen-2 VM, disk on F: by default, on the Default Switch so it shares the network with fleet-test.

.DESCRIPTION
  Server 2022 does NOT require a TPM (unlike Win11), so this is a plain Gen-2 build with the default
  Secure Boot template. The install itself is INTERACTIVE -- you complete Windows Setup at the console
  (pick "Standard (Desktop Experience)" for a GUI, set the Administrator password). Run ELEVATED.

.EXAMPLE
  .\Build-WindowsTestVM.ps1
  # then:  vmconnect.exe localhost fleet-win-test   (press a key at "boot from CD" IMMEDIATELY)
#>
[CmdletBinding()]
param(
  [string]$VMName        = "fleet-win-test",
  [string]$IsoPath       = "C:\isos\SW_DVD9_Win_Server_STD_CORE_2022_2108.9_64Bit_English_DC_STD_MLF_X23-14506.ISO",
  [string]$VhdxPath      = "F:\HyperV\fleet-win-test.vhdx",   # big Windows disk on F: (per the disk plan)
  [string]$SwitchName    = "Default Switch",
  [int]$Cpu              = 4,
  [int64]$MemoryStartup  = 4GB,
  [int64]$MemoryMin      = 2GB,
  [int64]$MemoryMax      = 8GB,
  [int64]$DiskSize       = 64GB
)
$ErrorActionPreference = "Stop"

if (-not (Test-Path $IsoPath)) { throw "Windows ISO not found: $IsoPath" }
if (Get-VM -Name $VMName -ErrorAction SilentlyContinue) {
  throw "VM '$VMName' already exists. Remove it first:  Stop-VM $VMName -TurnOff -Force; Remove-VM $VMName -Force"
}

$vhdDir = Split-Path -Parent $VhdxPath
if (-not (Test-Path $vhdDir)) { New-Item -ItemType Directory -Force -Path $vhdDir | Out-Null }
if (Test-Path $VhdxPath) { throw "VHDX already exists: $VhdxPath (remove it or pass a different -VhdxPath)" }

Write-Host "==> Creating $VMName : Gen2 | $Cpu vCPU | $([math]::Round($MemoryStartup/1GB))GB (dyn $([math]::Round($MemoryMin/1GB))-$([math]::Round($MemoryMax/1GB))) | $([math]::Round($DiskSize/1GB))GB disk on $vhdDir" -ForegroundColor Cyan

New-VHD -Path $VhdxPath -SizeBytes $DiskSize -Dynamic | Out-Null
New-VM -Name $VMName -Generation 2 -MemoryStartupBytes $MemoryStartup -VHDPath $VhdxPath -SwitchName $SwitchName | Out-Null
Set-VMProcessor -VMName $VMName -Count $Cpu
Set-VMMemory -VMName $VMName -DynamicMemoryEnabled $true -MinimumBytes $MemoryMin -MaximumBytes $MemoryMax -StartupBytes $MemoryStartup

# Attach the install ISO and boot from it. Gen2 default Secure Boot template (MicrosoftWindows) is correct
# for Windows Server 2022 -- leave it on.
$dvd = Add-VMDvdDrive -VMName $VMName -Path $IsoPath -Passthru
Set-VMFirmware -VMName $VMName -FirstBootDevice $dvd
# Integration services: guest IP reporting (so Get-VMNetworkAdapter shows the IP) + guest file copy.
Enable-VMIntegrationService -VMName $VMName -Name "Guest Service Interface" -ErrorAction SilentlyContinue
# Keep the ISO from auto-ejecting; checkpoints off for a throwaway test box.
Set-VM -VMName $VMName -AutomaticCheckpointsEnabled $false -ErrorAction SilentlyContinue

Start-VM -Name $VMName
Write-Host ""
Write-Host "Windows Server 2022 test VM started: $VMName" -ForegroundColor Green
Write-Host ""
Write-Host "NEXT (interactive install):" -ForegroundColor Cyan
Write-Host "  1. Connect NOW and press a key at 'Press any key to boot from CD':"
Write-Host "       vmconnect.exe localhost $VMName"
Write-Host "  2. In Setup pick: Windows Server 2022 Standard (Desktop Experience) -> Custom install ->"
Write-Host "     the single disk. Set the Administrator password. (~10-15 min.)"
Write-Host "  3. At the desktop, open an elevated PowerShell and enable OpenSSH so it can be driven remotely"
Write-Host "     (same as the Linux boxes) -- paste this:" -ForegroundColor Cyan
Write-Host '       Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0'
Write-Host '       Start-Service sshd; Set-Service sshd -StartupType Automatic'
Write-Host '       New-NetFirewallRule -Name sshd -DisplayName "OpenSSH Server" -Enabled True -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22'
Write-Host "  4. Find its IP (shares the Default Switch with fleet-test):"
Write-Host "       (Get-VMNetworkAdapter -VMName $VMName).IPAddresses"
Write-Host ""
Write-Host "Then we install ScreenConnect (human/setup/install-screenconnect-testfleet.ps1) + enroll fleetd."
