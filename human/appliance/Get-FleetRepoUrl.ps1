<#
.SYNOPSIS
  Emit a tokenized HTTPS clone URL for the private Fleet fork, using the GitHub CLI's auth token, so you can
  feed it to Build-FleetAppliance.ps1 -RepoUrl in one go.

.DESCRIPTION
  Prefer LOCAL source (Build-FleetAppliance.ps1 with no -RepoUrl) when you can — it needs no token and
  captures uncommitted work. Use this only for the remote-clone path.

  This returns the URL built from `gh auth token` (your GitHub CLI login token). That token carries your
  account's scopes, so treat the URL as a secret and prefer a short-lived, read-only fine-grained PAT for
  anything long-lived. The tokenized URL ends up in the VM's first-boot provision script (a secret at rest).

.EXAMPLE
  # One go: build using a token URL from gh
  .\Build-FleetAppliance.ps1 -GenerateSshKey -VhdxPath C:\HyperV\fleet-prod.vhdx `
    -RepoUrl (.\Get-FleetRepoUrl.ps1)
#>
[CmdletBinding()]
param(
  [string]$Repo = "humandevs/fleet"
)
$ErrorActionPreference = "Stop"

if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
  throw "GitHub CLI not found. Install it first:  winget install --id GitHub.cli"
}

# Authenticate once if needed (interactive: pick GitHub.com -> HTTPS -> login in browser).
gh auth status 2>$null
if ($LASTEXITCODE -ne 0) {
  Write-Host "Not logged in to gh - launching 'gh auth login' (choose HTTPS)..." -ForegroundColor Yellow
  gh auth login
}

$token = (gh auth token).Trim()
if (-not $token) { throw "gh returned no token. Run 'gh auth login' and retry." }

# Emit the clone URL (to stdout, so it can be captured or piped into -RepoUrl).
"https://$token@github.com/$Repo.git"
