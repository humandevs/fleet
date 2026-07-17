<#
.SYNOPSIS
  Fast Fleet iteration without a full rebuild: deploy a pre-built `fleet` binary to a running appliance VM
  (swap binary + restart), or pull a freshly-built binary FROM a builder VM to keep as the artifact.

.DESCRIPTION
  Fleet's server binary needs cgo (fts5/sqlite), so it must be built on Linux -- you can't cross-compile it
  on Windows. So the loop is: build once on a Linux builder VM (the appliance already has the toolchain),
  pull the binary here (-PullFrom), then deploy that one ~100 MB artifact to any runtime VM. The frontend is
  embedded in the binary, so it's the only app file that changes. The runtime VM keeps its MySQL/Redis
  (compose) + config, so a redeploy is just: replace /usr/local/bin/fleet and restart the service.

  Uses the Windows OpenSSH client (ssh/scp) with the appliance key + passwordless sudo -- no Ansible/Linux
  controller needed. Finds the VM IP the same way Watch-FleetVM.ps1 does (KVP, else host ARP by MAC).

.EXAMPLE
  # 1) capture the artifact from the builder VM after it has built Fleet:
  .\Push-FleetState.ps1 -VMName fleet-prod -PullFrom
  # 2) deploy that artifact to a runtime VM and restart:
  .\Push-FleetState.ps1 -VMName fleet-prod
#>
[CmdletBinding()]
param(
  [string]$VMName    = "fleet-prod",
  [string]$IP        = "",
  [string]$User      = "fleetadmin",
  [string]$KeyPath   = "C:\HyperV\fleet-prod-ssh\fleet_ceplus_ed25519_a",
  [string]$Artifact  = "$PSScriptRoot\artifacts\fleet",   # local path to the built linux binary
  [string]$RemoteBin = "/usr/local/bin/fleet",             # where the runtime VM runs it from
  [string]$RemoteBuilt = "/opt/fleet-src/build/fleet",     # where the builder VM leaves a fresh build
  [switch]$PullFrom,                                         # pull the built binary FROM the VM instead of deploying
  [switch]$Build,                                            # sync local source to the VM, build it there, restart
  [string]$RepoSource = ""                                  # -Build source; default = repo root (human\appliance\..\..)
)
$ErrorActionPreference = "Stop"

function Resolve-IP {
  param([string]$Name)
  $a = Get-VMNetworkAdapter -VMName $Name -ErrorAction SilentlyContinue
  $ip = $a.IPAddresses | Where-Object { $_ -match '^\d{1,3}(\.\d{1,3}){3}$' -and $_ -notmatch '^169\.254\.' } | Select-Object -First 1
  if ($ip) { return $ip }
  $mac = $a.MacAddress
  if ($mac) {
    $macFmt = ($mac -replace '(.{2})(?=.)', '$1-').ToUpper()
    return (Get-NetNeighbor -LinkLayerAddress $macFmt -ErrorAction SilentlyContinue |
      Where-Object { $_.AddressFamily -eq 'IPv4' -and $_.IPAddress -notmatch '^169\.254\.' -and $_.State -ne 'Unreachable' } |
      Select-Object -First 1).IPAddress
  }
  return $null
}

if (-not $IP) { $IP = Resolve-IP $VMName }
if (-not $IP) { throw "Could not find the IP for $VMName. Pass -IP, or read it on the VM: ip -4 addr show scope global" }
if (-not (Test-Path $KeyPath)) { throw "SSH key not found: $KeyPath (pass -KeyPath)" }
$sshTarget = "$User@$IP"
$sshOpts   = @("-i", $KeyPath, "-o", "StrictHostKeyChecking=accept-new")

if ($Build) {
  # Sync local source to the builder VM, build it there (warm caches -> incremental), install + restart.
  if (-not $RepoSource) { $RepoSource = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path }
  if (-not (Test-Path (Join-Path $RepoSource "go.mod"))) { throw "RepoSource '$RepoSource' is not the Fleet repo (no go.mod). Pass -RepoSource." }

  Write-Host "==> Packaging source from $RepoSource ..." -ForegroundColor Cyan
  # Fresh unique stage dir each run so a stale/AV-locked prior tarball can't block tar's output (see notes
  # in Build-FleetAppliance.ps1). Sweep old ones best-effort.
  Get-ChildItem $env:TEMP -Directory -Filter "fleet-build-push-*" -ErrorAction SilentlyContinue |
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue
  $stage = Join-Path $env:TEMP ("fleet-build-push-" + [Guid]::NewGuid().ToString("N").Substring(0, 8))
  New-Item -ItemType Directory -Force -Path $stage | Out-Null
  $tgz = Join-Path $stage "fleet-src.tar.gz"
  $exFile = Join-Path $stage "excludes.txt"
  # NOTE: do NOT exclude 'build' -- bsdtar matches it as ANY path component, which also drops the real
  # source package orbit/pkg/build (the server fails to compile: "no required module provides package
  # .../orbit/pkg/build"). Same note in Build-FleetAppliance.ps1; keep these lists in sync.
  @('.git','.git/*','*/.git','*/.git/*','*node_modules*','.cache','.cache/*','*/.cache','*/.cache/*','*.vhdx') |
    Set-Content -Encoding Ascii $exFile
  $tarExe = Join-Path $env:SystemRoot "System32\tar.exe"
  & $tarExe -czf $tgz -C $RepoSource --exclude-from=$exFile .
  if ($LASTEXITCODE -ne 0) { throw "tar failed packaging the source." }
  Write-Host ("    {0} MB source -> $($sshTarget):/tmp/fleet-src.tar.gz" -f [math]::Round((Get-Item $tgz).Length/1MB))
  & scp @sshOpts $tgz "$($sshTarget):/tmp/fleet-src.tar.gz"
  if ($LASTEXITCODE -ne 0) { throw "scp of source failed." }

  # Extract over /opt/fleet-src (keeps node_modules/build/Go caches for an incremental build), build, install,
  # restart, smoke. NOTE: tar-extract does not delete files removed locally; for a clean tree, re-provision.
  $remoteBuild = @'
set -e
sudo mkdir -p /opt/fleet-src
sudo chown -R "$(whoami)" /opt/fleet-src
tar -xzf /tmp/fleet-src.tar.gz -C /opt/fleet-src && rm -f /tmp/fleet-src.tar.gz
cd /opt/fleet-src
# make generate runs 'git clean -fx assets' which needs a repo; recreate one if .git is absent.
[ -d .git ] || { git init -q && git add -A && git -c user.email=appliance@fleet.local -c user.name=fleet-appliance commit -q -m "appliance base"; }
export PATH=$PATH:/usr/local/go/bin
echo "== make deps ==";     make deps
echo "== make generate =="; make generate
echo "== make build ==";    make build
sudo install -m 0755 build/fleet __BIN__
sudo systemctl restart fleet
echo "waiting for /healthz..."
for i in $(seq 1 24); do curl -fsk https://127.0.0.1:8080/healthz >/dev/null 2>&1 && { echo HEALTHY; exit 0; }; sleep 5; done
echo "NOT-HEALTHY - check: sudo journalctl -u fleet -n 50"; exit 1
'@ -replace '__BIN__', $RemoteBin

  Write-Host "==> Building on $sshTarget (make generate + build; warm caches ~2-4 min)..." -ForegroundColor Cyan
  & ssh @sshOpts $sshTarget $remoteBuild
  if ($LASTEXITCODE -eq 0) { Write-Host "Built + deployed. Fleet healthy: https://$($IP):8080" -ForegroundColor Green }
  else { Write-Host "Build/deploy did not report healthy. Check journalctl on the VM." -ForegroundColor Yellow }
  return
}

if ($PullFrom) {
  # Capture the freshly-built binary from the builder VM as the local artifact.
  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Artifact) | Out-Null
  Write-Host "==> Pulling $RemoteBuilt from $sshTarget -> $Artifact" -ForegroundColor Cyan
  & scp @sshOpts "$($sshTarget):$RemoteBuilt" $Artifact
  if ($LASTEXITCODE -ne 0) { throw "scp pull failed. Has the builder finished 'make build'? Check $RemoteBuilt on the VM." }
  Write-Host ("    Artifact: {0} ({1} MB)" -f $Artifact, [math]::Round((Get-Item $Artifact).Length/1MB)) -ForegroundColor Green
  return
}

# Deploy the artifact to the runtime VM and restart.
if (-not (Test-Path $Artifact)) {
  throw "No artifact at $Artifact. Build on a Linux builder VM first, then: .\Push-FleetState.ps1 -PullFrom"
}
Write-Host "==> Deploying $Artifact -> $($sshTarget):$RemoteBin" -ForegroundColor Cyan
& scp @sshOpts $Artifact "$($sshTarget):/tmp/fleet.new"
if ($LASTEXITCODE -ne 0) { throw "scp push failed." }

# Install atomically, restart, and smoke-check -- all in one sudo session.
$remote = @"
set -e
sudo install -m 0755 /tmp/fleet.new '$RemoteBin' && rm -f /tmp/fleet.new
sudo systemctl restart fleet
echo 'waiting for /healthz...'
for i in \$(seq 1 24); do
  if curl -fsk https://127.0.0.1:8080/healthz >/dev/null 2>&1; then echo 'HEALTHY'; exit 0; fi
  sleep 5
done
echo 'NOT-HEALTHY - check: sudo journalctl -u fleet -n 50'; exit 1
"@
Write-Host "==> Installing + restarting fleet on $sshTarget ..." -ForegroundColor Cyan
& ssh @sshOpts $sshTarget $remote
if ($LASTEXITCODE -eq 0) {
  Write-Host "Deployed. Fleet is healthy: https://$($IP):8080" -ForegroundColor Green
} else {
  Write-Host "Deployed, but Fleet did not report healthy. Check journalctl on the VM." -ForegroundColor Yellow
}
