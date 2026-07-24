# Install the Human-ISM ScreenConnect access agent into the 'TestFleet' access group.
# The group is set by CustomProperty1=TestFleet, baked into the installer via the first &c= in the build
# URL (repeated c= values populate CustomProperty1..8 in order). Run in an elevated PowerShell on the
# Windows test endpoint. This is the manual analog of the collector's WindowsInstallScript
# (server/community/screenconnect/screenconnect.go); once wired, orbit runs the same thing as SYSTEM.
$ErrorActionPreference = 'Stop'

# Grouped build URL: e=Access (unattended access), y=Guest, c=TestFleet (CustomProperty1), rest empty.
$url = 'https://screenconnect.human-ism.com/Bin/Human-ISM.RemoteScreenConnect.ClientSetup.msi?e=Access&y=Guest&c=TestFleet&c=&c=&c=&c=&c=&c=&c='
$msi = Join-Path $env:TEMP 'sc-testfleet.msi'

Write-Host "==> Downloading grouped installer (CustomProperty1=TestFleet)..." -ForegroundColor Cyan
Invoke-WebRequest -Uri $url -OutFile $msi -UseBasicParsing

Write-Host "==> Installing silently..." -ForegroundColor Cyan
$p = Start-Process msiexec.exe -ArgumentList "/i `"$msi`" /qn /norestart REBOOT=REALLYSUPPRESS" -Wait -PassThru
Write-Host "    msiexec exit code: $($p.ExitCode)  (0 = success, 3010 = success/reboot-suppressed)"

Write-Host "==> Verifying the installed service..." -ForegroundColor Cyan
$svc = Get-Service | Where-Object { $_.Name -like 'ScreenConnect Client*' -or $_.DisplayName -like 'ScreenConnect Client*' } | Select-Object -First 1
if ($svc) {
  Write-Host "    Service: $($svc.Name)  [$($svc.Status)]" -ForegroundColor Green
  # The service is named "ScreenConnect Client (<InstanceID>)". That InstanceID is what the collector
  # needs for local (osquery) presence detection so a competitor's side-by-side agent isn't counted as
  # ours -- set it as FLEET_COMMUNITY_SCREENCONNECT_INSTANCE_ID.
  if ($svc.Name -match 'ScreenConnect Client \(([0-9A-Fa-f]+)\)') {
    Write-Host "    InstanceID (-> FLEET_COMMUNITY_SCREENCONNECT_INSTANCE_ID): $($Matches[1])" -ForegroundColor Green
  }
  Write-Host "    This machine should now appear in ScreenConnect under the 'TestFleet' group, and the"
  Write-Host "    RESTful API Manager GetSessionsByFilter should return it (that's what the collector polls)."
} else {
  Write-Host "    WARNING: no 'ScreenConnect Client' service found -- check the install / the build URL." -ForegroundColor Yellow
}
