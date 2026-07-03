// Package screenconnect is the community ScreenConnect (ConnectWise Control) provider. It (a) generates
// org-linked access-agent install commands for the free scripts pipeline, and (b) reports remote-access
// coverage into the host coverage matrix. MIT/free — it does not require the enterprise license and
// coexists with it. See human/setup/screenconnect.md.
package screenconnect

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/fleetdm/fleet/v4/server/fleet"
)

// Config is the per-instance ScreenConnect configuration. Secrets must be stored envelope-encrypted
// (human/RISK-REGISTER.md #3), never plaintext.
type Config struct {
	// InstanceURL is the ScreenConnect base URL, e.g. "https://example.screenconnect.com".
	InstanceURL string
	// AccessSecret is the RESTful API Manager shared secret (sent as the CTRLAuthHeader header) used to
	// poll session/online status. Optional for deployment-only use.
	AccessSecret string
	// InstallerName is the base name of the org's access-agent build (the "<Name>" in
	// /Bin/<Name>.ClientSetup.msi). Defaults to "ScreenConnect".
	InstallerName string
}

// OrgLink maps a Fleet team/site to the ScreenConnect company/site custom properties that link a device
// to the correct organization on install (CustomProperty1 = Company, CustomProperty2 = Site).
type OrgLink struct {
	Company string
	Site    string
}

// Provider implements community.HostStatusProvider for remote access.
type Provider struct {
	cfg Config
}

// New returns a ScreenConnect provider.
func New(cfg Config) *Provider {
	if cfg.InstallerName == "" {
		cfg.InstallerName = "ScreenConnect"
	}
	return &Provider{cfg: cfg}
}

// Source implements community.HostStatusProvider.
func (p *Provider) Source() string { return "screenconnect" }

// Categories implements community.HostStatusProvider.
func (p *Provider) Categories() []fleet.IntegrationCategory {
	return []fleet.IntegrationCategory{fleet.IntegrationCategoryRemoteAccess}
}

// InstallerURL builds the org-linked access-agent installer URL for a host. The repeated c= values
// populate ScreenConnect CustomProperty1..N (Company, Site) — this is what links the device to the
// right organization on install. t= sets the session name (the Fleet hostname); e=Access selects the
// unattended access session type. See human/setup/screenconnect.md.
func (p *Provider) InstallerURL(hostname string, org OrgLink) string {
	base := strings.TrimRight(p.cfg.InstanceURL, "/")
	return fmt.Sprintf(
		"%s/Bin/%s.ClientSetup.msi?e=Access&y=Guest&t=%s&c=%s&c=%s",
		base,
		url.PathEscape(p.cfg.InstallerName),
		url.QueryEscape(hostname),
		url.QueryEscape(org.Company),
		url.QueryEscape(org.Site),
	)
}

// WindowsInstallScript returns a PowerShell script that downloads and silently installs the org-linked
// access agent. It runs via the orbit scripts engine as SYSTEM (no software-installer pipeline
// required), so it works on the free/community core.
func (p *Provider) WindowsInstallScript(hostname string, org OrgLink) string {
	return fmt.Sprintf(`$ErrorActionPreference = "Stop"
$url = "%s"
$msi = "$env:TEMP\screenconnect-access.msi"
Invoke-WebRequest -Uri $url -OutFile $msi -UseBasicParsing
& msiexec.exe /i $msi /quiet /qn /norestart REBOOT=REALLYSUPPRESS
`, p.InstallerURL(hostname, org))
}

// Sync polls ScreenConnect for online/last-connected status and upserts remote_access coverage cells.
// Not yet implemented — the RESTful API Manager integration is a follow-up (see
// human/setup/screenconnect.md). Kept as the poller seam so wiring is ready.
func (p *Provider) Sync(ctx context.Context, ds fleet.Datastore) error {
	// TODO: GetSessionsByFilter via the RESTful API Manager (header CTRLAuthHeader = AccessSecret),
	// map GuestConnectedCount>0 to IntegrationStateProtected, and upsert per host by stored SessionID.
	_ = ctx
	_ = ds
	return nil
}
