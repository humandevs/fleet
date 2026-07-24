// Package screenconnect is the community ScreenConnect (ConnectWise Control) provider. It (a) generates
// org-linked access-agent install commands for the free scripts pipeline, and (b) reports remote-access
// coverage into the host coverage matrix. MIT/free — it does not require the enterprise license and
// coexists with it. See human/setup/screenconnect.md.
package screenconnect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fleetdm/fleet/v4/pkg/fleethttp"
	"github.com/fleetdm/fleet/v4/server/community"
	"github.com/fleetdm/fleet/v4/server/contexts/ctxerr"
	"github.com/fleetdm/fleet/v4/server/fleet"
)

// Config is the per-instance ScreenConnect configuration. Secrets must be stored envelope-encrypted
// (human/RISK-REGISTER.md #3), never plaintext.
type Config struct {
	// InstanceURL is the ScreenConnect server base URL. For our self-hosted instance this is our own
	// server (e.g. "https://remote.example.com" or "https://example.com:8040") — there is no
	// *.screenconnect.com default, so it is REQUIRED (see Validate). The per-org installer downloaded from
	// this URL embeds the server's relay address, certificate thumbprint, and join key, so an installed
	// agent auto-registers without an API credential.
	InstanceURL string
	// RelayHost and RelayPort override the agent's phone-home relay endpoint (installer h=/p= params).
	// Needed for self-hosted deployments where the relay endpoint differs from the web URL — e.g. the web
	// UI is reverse-proxied on 443 but the relay listens on the server host:8041. Leave zero to use the
	// values baked into the installer by the server (correct when the installer is downloaded directly
	// from InstanceURL). Cloud instances never need these.
	RelayHost string
	RelayPort int
	// Thumbprint overrides the server certificate fingerprint (installer k= param). Set only for a
	// self-hosted server with a self-signed cert when you also override RelayHost. Leave empty to use the
	// installer default.
	Thumbprint string
	// InstanceID is OUR ScreenConnect instance's identifier — the 16-hex string ScreenConnect derives from
	// this server and stamps into every agent it builds. The Windows service is named
	// "ScreenConnect Client (<InstanceID>)" and it installs under
	// "C:\Program Files (x86)\ScreenConnect Client (<InstanceID>)\". It is REQUIRED for local (osquery)
	// presence detection: multiple vendors' ScreenConnect agents coexist side-by-side on one host, so
	// matching "any ScreenConnect service" would report a competitor's agent as ours. This is the instance
	// id, NOT Thumbprint (that is the TLS cert fingerprint / installer k= param). Find it in the
	// ScreenConnect admin console or as the "(...)" suffix of an already-installed service. Leave empty to
	// skip local presence detection — the API-poll coverage path (InstanceURL + AccessSecret) is already
	// instance-scoped and does not need it.
	InstanceID string
	// AccessSecret is the RESTful API Manager shared secret (sent as the CTRLAuthHeader header) used to
	// poll session/online status. Optional: if empty, Collect is a no-op (deployment-only mode).
	AccessSecret string
	// APIPath is the RESTful API Manager service path for GetSessionsByFilter. The extension GUID is FIXED
	// (2d558935-686a-4bd0-9991-07539f5fe749 — the same on every install, per the extension docs), so this
	// defaults (see New) to the standard GetSessionsByFilter path; override only if a future extension
	// version changes it. See human/setup/screenconnect.md.
	APIPath string
	// SessionFilter is the GetSessionsByFilter argument — a ScreenConnect session-filter expression scoping
	// which sessions to poll, e.g. "CustomProperty1 = 'TestFleet'" (a specific access group) or
	// "SessionType = 'Access'" (ALL access agents — can be thousands on a busy server). REQUIRED for
	// polling: the API rejects a null/empty filter, so Collect no-ops when it is empty (deployment-only).
	SessionFilter string
	// InstallerName is the base name of the org's access-agent build (the "<Name>" in
	// /Bin/<Name>.ClientSetup.msi). Defaults to "ScreenConnect".
	InstallerName string
}

// Validate checks required configuration. InstanceURL is required and must be an absolute http(s) URL:
// self-hosted has no conventional default domain, so an unset/typo'd URL must fail loudly rather than
// produce a broken "/Bin/..." install command.
func (c Config) Validate() error {
	raw := strings.TrimSpace(c.InstanceURL)
	if raw == "" {
		return fleet.NewInvalidArgumentError("screenconnect.instance_url",
			"is required (set your self-hosted ScreenConnect base URL, e.g. https://remote.example.com)")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fleet.NewInvalidArgumentError("screenconnect.instance_url",
			"must be an absolute http(s) URL, e.g. https://remote.example.com:8040")
	}
	return nil
}

// maxCustomProperties is ScreenConnect's fixed limit of custom property fields per session.
const maxCustomProperties = 8

// OrgLink carries the ScreenConnect custom properties (a.k.a. CustomField / CustomProperty 1..8) baked
// into the installer as repeated c= values, in order. ScreenConnect session-group filters match on these
// to route a device into a group: by convention CustomProperty1 = client/company (the group key),
// CustomProperty2 = site, then department, device type, etc. Only position matters on install — the human
// labels are configured server-side.
type OrgLink struct {
	// CustomProperties are CustomProperty1..8 in order (values past 8 are dropped). Index 0 ->
	// CustomProperty1, the value group filters usually key on. Interior empties are preserved (c= is
	// positional); trailing empties are omitted.
	CustomProperties []string
}

// Org is a convenience constructor for the common case: CustomProperty1 = client (the group key),
// CustomProperty2 = site, plus any further positional properties (department, device type, ...).
func Org(client, site string, extra ...string) OrgLink {
	return OrgLink{CustomProperties: append([]string{client, site}, extra...)}
}

// props returns the custom properties to emit: capped at ScreenConnect's 8, with trailing empties trimmed
// (interior empties kept, since c= is positional).
func (o OrgLink) props() []string {
	p := o.CustomProperties
	if len(p) > maxCustomProperties {
		p = p[:maxCustomProperties]
	}
	end := len(p)
	for end > 0 && p[end-1] == "" {
		end--
	}
	return p[:end]
}

// Provider implements community.HostStatusProvider for remote access.
type Provider struct {
	cfg Config
}

// defaultAPIPath is the RESTful API Manager GetSessionsByFilter service path. The extension GUID is fixed
// across every instance (per the extension docs), so this works without per-instance configuration.
const defaultAPIPath = "/App_Extensions/2d558935-686a-4bd0-9991-07539f5fe749/Service.ashx/GetSessionsByFilter"

// New returns a ScreenConnect provider.
func New(cfg Config) *Provider {
	if cfg.InstallerName == "" {
		cfg.InstallerName = "ScreenConnect"
	}
	if cfg.APIPath == "" {
		cfg.APIPath = defaultAPIPath
	}
	return &Provider{cfg: cfg}
}

// Source implements community.HostStatusProvider.
func (p *Provider) Source() string { return "screenconnect" }

// Categories implements community.HostStatusProvider.
func (p *Provider) Categories() []fleet.IntegrationCategory {
	return []fleet.IntegrationCategory{fleet.IntegrationCategoryRemoteAccess}
}

// ServiceName returns the Windows service / installed-program name of OUR access agent —
// "ScreenConnect Client (<InstanceID>)". This is how we tell our instance apart from other vendors'
// ScreenConnect agents on the same host. Returns "" when InstanceID is unset (presence detection disabled).
func (p *Provider) ServiceName() string {
	if p.cfg.InstanceID == "" {
		return ""
	}
	return fmt.Sprintf("ScreenConnect Client (%s)", p.cfg.InstanceID)
}

// PresenceQuery returns an osquery query that passes (returns a row) only when OUR access agent is present
// AND its service is RUNNING on a Windows host — matched by InstanceID so a side-by-side competitor's
// ScreenConnect never counts as ours. Use it as the Fleet policy that gates the keep-installed reinstall
// (policy fails ⇒ run WindowsInstallScript). Returns "" when InstanceID is unset.
func (p *Provider) PresenceQuery() string {
	name := p.ServiceName()
	if name == "" {
		return ""
	}
	// Instance IDs are hex, but escape defensively — a stray quote must not break out of the literal.
	return fmt.Sprintf("SELECT 1 FROM services WHERE name = '%s' AND status = 'RUNNING';",
		strings.ReplaceAll(name, "'", "''"))
}

// InstallerURL builds the org-linked access-agent installer URL for a host. t= sets the session name (the
// Fleet hostname); e=Access selects the unattended access session type; the repeated c= values populate
// ScreenConnect CustomProperty1..8 in order — this is what links the device to the right group
// (client/site) on install. See human/setup/screenconnect.md.
func (p *Provider) InstallerURL(hostname string, org OrgLink) string {
	base := strings.TrimRight(p.cfg.InstanceURL, "/")
	u := fmt.Sprintf(
		"%s/Bin/%s.ClientSetup.msi?e=Access&y=Guest&t=%s",
		base,
		url.PathEscape(p.cfg.InstallerName),
		url.QueryEscape(hostname),
	)
	// Repeated c= values set CustomProperty1..8 in order — the group/client/site handoff.
	for _, c := range org.props() {
		u += "&c=" + url.QueryEscape(c)
	}
	// Self-hosted relay/thumbprint overrides — appended only when set, so the default path (download the
	// installer from InstanceURL, which bakes in the server's own relay) is byte-for-byte unchanged and
	// cloud is unaffected. Use these for the split web/relay topology (reverse proxy on 443, relay on 8041).
	if p.cfg.RelayHost != "" {
		u += "&h=" + url.QueryEscape(p.cfg.RelayHost)
	}
	if p.cfg.RelayPort > 0 {
		u += fmt.Sprintf("&p=%d", p.cfg.RelayPort)
	}
	if p.cfg.Thumbprint != "" {
		u += "&k=" + url.QueryEscape(p.cfg.Thumbprint)
	}
	return u
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

// EnsureSessionGroup idempotently provisions a ScreenConnect Access session group so installs land in the
// right place in the ScreenConnect UI. It is meant to be called when a Fleet client (team) or site is
// created or renamed: it ensures a session group whose filter is `CustomProperty1 = '<client>'` exists,
// plus a child group per site additionally matching `CustomProperty2 = '<site>'`. Note the division of
// labor: a device is actually ROUTED by the CustomProperty values baked into its installer (see
// InstallerURL / OrgLink); this call only ensures the matching group (a saved filter view) exists so those
// devices show up grouped rather than loose.
//
// NOT YET IMPLEMENTED. Confidence is low: the RESTful API Manager exposes session read/command methods, but
// session-GROUP CRUD is an admin/config surface. Provisioning options, in order of preference:
//  1. A supported RESTful API Manager method, if the installed extension exposes SessionGroup CRUD.
//  2. Manage the server config directly (write SessionGroup elements) — viable because we self-host.
//  3. Internal page service (version-fragile) as a last resort — avoid as primary.
//
// Kept as the provisioning seam so the Fleet team/site lifecycle hook has something to call. See
// human/setup/screenconnect.md § "Group provisioning".
func (p *Provider) EnsureSessionGroup(ctx context.Context, client string, sites ...string) error {
	// TODO: create/patch the SessionGroup(s) via the chosen surface above. Idempotent by group name.
	_ = ctx
	_ = client
	_ = sites
	return nil
}

// apiSession is the subset of the RESTful API Manager GetSessionsByFilter session object we consume.
// Field names + shapes are from the live response (verified 2026-07-23): the machine name is nested under
// GuestInfo, and there is NO GuestConnectedCount — current guest connectivity is derived from the last
// connect/disconnect event times. Parsed defensively; unknown fields are ignored.
type apiSession struct {
	SessionID string `json:"SessionID"`
	Name      string `json:"Name"`
	IsEnded   bool   `json:"IsEnded"`
	GuestInfo struct {
		MachineName string `json:"MachineName"`
	} `json:"GuestInfo"`
	LastGuestConnectedEventTime    scTime `json:"LastGuestConnectedEventTime"`
	LastGuestDisconnectedEventTime scTime `json:"LastGuestDisconnectedEventTime"`
}

// scTime parses ScreenConnect's event timestamps. Real values are RFC3339 with fractional seconds and a
// Z (e.g. "2026-07-19T06:41:52.6169911Z"), but a "never happened" event serializes as the .NET
// DateTime.MinValue sentinel "0001-01-01T00:00:00" — note: NO timezone, which Go's RFC3339 parser
// rejects (and would fail the entire decode). We map the sentinel/empty to the zero time.
type scTime struct{ time.Time }

func (t *scTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" || strings.HasPrefix(s, "0001-01-01") {
		t.Time = time.Time{}
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339Nano, s); err == nil {
		t.Time = parsed
		return nil
	}
	// Zone-less fallback (assume UTC) for any value that omits the Z.
	parsed, err := time.Parse("2006-01-02T15:04:05.999999999", s)
	if err != nil {
		return err
	}
	t.Time = parsed.UTC()
	return nil
}

// guestOnline reports whether the guest (the managed machine's agent) is currently connected: its last
// connect event is more recent than its last disconnect (or it connected and never disconnected).
func (s apiSession) guestOnline() bool {
	if s.LastGuestConnectedEventTime.IsZero() {
		return false
	}
	return s.LastGuestDisconnectedEventTime.IsZero() ||
		s.LastGuestConnectedEventTime.After(s.LastGuestDisconnectedEventTime.Time)
}

// Collect implements community.Collector: it polls ScreenConnect for current sessions and maps each to a
// remote_access coverage reading keyed by the session name (which is the Fleet hostname, since we set
// t=<hostname> at install). A connected guest ⇒ protected; a known-but-offline session ⇒ at_risk. Returns
// nil (no-op) when polling isn't configured (no AccessSecret/APIPath) so a deployment-only instance is
// valid. It performs no DB access — the community Runner resolves hostnames to hosts and persists.
func (p *Provider) Collect(ctx context.Context) ([]community.HostStatusReport, error) {
	if p.cfg.AccessSecret == "" || p.cfg.SessionFilter == "" {
		return nil, nil // deployment-only mode (no polling configured)
	}
	sessions, err := p.getSessions(ctx)
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "screenconnect collect sessions")
	}
	reports := make([]community.HostStatusReport, 0, len(sessions))
	for _, s := range sessions {
		if s.IsEnded {
			continue // session closed / agent removed — no coverage to report
		}
		// The managed machine is GuestInfo.MachineName; fall back to the session Name (we set t=<hostname>
		// at install, so for our own deploys the Name is the Fleet hostname too).
		name := s.GuestInfo.MachineName
		if name == "" {
			name = s.Name
		}
		if name == "" {
			continue // no host key to resolve against
		}
		state, detail := fleet.IntegrationStateAtRisk, "agent offline"
		if s.guestOnline() {
			state, detail = fleet.IntegrationStateProtected, "agent online"
		}
		reports = append(reports, community.HostStatusReport{
			Identifier:     name,
			IdentifierKind: community.IdentifierHostname,
			Category:       fleet.IntegrationCategoryRemoteAccess,
			State:          state,
			Detail:         detail,
		})
	}
	return reports, nil
}

// maxResponseBytes caps how much of the session-list response body we read (32 MB — far above any real
// session list): a malicious or spoofed RESTful API Manager endpoint must not be able to OOM the
// collector with an unbounded body.
const maxResponseBytes = 32 << 20

// getSessions calls the RESTful API Manager session-list method (POST JSON, shared secret in the
// CTRLAuthHeader header) and returns the parsed sessions.
func (p *Provider) getSessions(ctx context.Context) ([]apiSession, error) {
	endpoint := strings.TrimRight(p.cfg.InstanceURL, "/") + p.cfg.APIPath
	// GetSessionsByFilter(string sessionFilter): the one argument is passed as a single-element JSON array.
	// One bulk call scoped by the filter, on a minutes cadence (see human/setup/screenconnect.md).
	reqBody, err := json.Marshal([]string{p.cfg.SessionFilter})
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "encode session filter")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "build request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("CTRLAuthHeader", p.cfg.AccessSecret)

	client := fleethttp.NewClient(fleethttp.WithTimeout(30 * time.Second))
	resp, err := client.Do(req)
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "do request")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, ctxerr.Errorf(ctx, "screenconnect api status %d", resp.StatusCode)
	}
	// Read at most maxResponseBytes+1 so an over-limit body is distinguishable from one exactly at the
	// limit, and fail loudly rather than decode a truncated list.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "read response body")
	}
	if len(body) > maxResponseBytes {
		return nil, ctxerr.Errorf(ctx, "screenconnect api response exceeds %d bytes", maxResponseBytes)
	}
	var sessions []apiSession
	if err := json.Unmarshal(body, &sessions); err != nil {
		return nil, ctxerr.Wrap(ctx, err, "decode sessions")
	}
	return sessions, nil
}
