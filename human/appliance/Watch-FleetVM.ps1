<#
.SYNOPSIS
  Poll a Fleet appliance VM for its IPv4, print how to connect, then wait until Fleet answers on :8080.

.DESCRIPTION
  Finds the IP two ways so it works even on a minimal guest with no Hyper-V daemons:
    1. Get-VMNetworkAdapter .IPAddresses  (needs hyperv-daemons/KVP in the guest), else
    2. the host's neighbor (ARP) cache, matched by the VM's MAC (no guest agent required).

.EXAMPLE
  .\Watch-FleetVM.ps1 -KeyPath C:\HyperV\fleet-prod-ssh\fleet_ceplus_ed25519_a
#>
[CmdletBinding()]
param(
  [string]$VMName     = "fleet-prod",
  [string]$User       = "fleetadmin",
  [string]$KeyPath    = "",          # e.g. C:\HyperV\fleet-prod-ssh\fleet_ceplus_ed25519_a
  [int]$TimeoutMin    = 40
)
$ErrorActionPreference = "SilentlyContinue"

function Get-VMIPv4 {
  param([string]$Name)
  $a = Get-VMNetworkAdapter -VMName $Name
  # 1) KVP-reported (needs hyperv-daemons in the guest)
  $ip = $a.IPAddresses | Where-Object { $_ -match '^\d{1,3}(\.\d{1,3}){3}$' -and $_ -notmatch '^169\.254\.' } | Select-Object -First 1
  if ($ip) { return $ip }
  # 2) host neighbor/ARP cache, matched by MAC (works without any guest agent)
  $mac = $a.MacAddress
  if ($mac) {
    $macFmt = ($mac -replace '(.{2})(?=.)', '$1-').ToUpper()
    $ip = (Get-NetNeighbor -LinkLayerAddress $macFmt |
      Where-Object { $_.AddressFamily -eq 'IPv4' -and $_.IPAddress -notmatch '^169\.254\.' -and $_.State -ne 'Unreachable' } |
      Select-Object -First 1).IPAddress
    if ($ip) { return $ip }
  }
  return $null
}

$deadline = (Get-Date).AddMinutes($TimeoutMin)
Write-Host "Watching $VMName for an IPv4 address..." -ForegroundColor Cyan
$ip = $null
while (-not $ip -and (Get-Date) -lt $deadline) {
  $ip = Get-VMIPv4 $VMName
  if (-not $ip) { Write-Host "." -NoNewline; Start-Sleep 5 }
}
Write-Host ""
if (-not $ip) {
  Write-Host "No IP found via the host after $TimeoutMin min." -ForegroundColor Yellow
  Write-Host "Read it on the VM console instead:  ip -4 addr show scope global"
  return
}

Write-Host "VM IP: $ip" -ForegroundColor Green
if ($KeyPath) { Write-Host ("SSH:   ssh -i {0} {1}@{2}" -f $KeyPath, $User, $ip) }
else          { Write-Host ("SSH:   ssh {0}@{1}" -f $User, $ip) }
Write-Host "UI:    https://$($ip):8080  (self-signed cert warning is expected)"
Write-Host ""
Write-Host "Waiting for Fleet to listen on :8080 (first-boot build, ~15 min)..." -ForegroundColor Cyan
while ((Get-Date) -lt $deadline) {
  if (Test-NetConnection $ip -Port 8080 -InformationLevel Quiet -WarningAction SilentlyContinue) {
    Write-Host ""
    Write-Host "Fleet is UP: https://$($ip):8080" -ForegroundColor Green
    Write-Host ""
    Write-Host "  ============================================================" -ForegroundColor Yellow
    Write-Host "  !!  BACK UP YOUR SECRETS NOW" -ForegroundColor Yellow
    Write-Host "  ------------------------------------------------------------" -ForegroundColor Yellow
    Write-Host "  This appliance just generated an IRREPLACEABLE key" -ForegroundColor Yellow
    Write-Host "  (fleet_server_private_key) plus its DB passwords. Lose the" -ForegroundColor Yellow
    Write-Host "  key and Fleet can NEVER decrypt what it stored (MDM +" -ForegroundColor Yellow
    Write-Host "  integration secrets). See human/SECRETS.md." -ForegroundColor Yellow
    Write-Host "  ------------------------------------------------------------" -ForegroundColor Yellow
    $sshArgs = if ($KeyPath) { "-i `"$KeyPath`" " } else { "" }
    Write-Host "  1) Copy the vault off the VM (it's root-owned, so sudo cat):" -ForegroundColor Yellow
    Write-Host "       ssh $sshArgs$User@$ip 'sudo cat /opt/fleet-src/human/secrets/vault.yml' > $VMName-vault-BACKUP.yml"
    Write-Host "  2) Put fleet_server_private_key into your password manager (OFF this machine)." -ForegroundColor Yellow
    Write-Host "  ============================================================" -ForegroundColor Yellow
    return
  }
  Write-Host "." -NoNewline; Start-Sleep 10
}
Write-Host ""
Write-Host "Port 8080 not open yet - provisioning may still be building." -ForegroundColor Yellow
Write-Host "On the VM:  sudo tail -f /var/log/fleet-firstboot.log"
