// Package action1 is the community Action1 (patch management / RMM) provider. Two layers: (A) deploy the
// Action1 Windows agent via the org-specific download URL through the orbit script pipeline, and (B) poll
// the Action1 REST API for managed-endpoint patch posture, reporting the "patching" coverage column
// (up-to-date → protected; missing updates or a silent agent → at_risk; no agent → not_installed). The
// at_risk "no successful check-in recently" signal is what a Views/Triggers automation (see
// human/RFC-coverage-dashboards-and-bundles.md §10) alerts and auto-repairs on. Auth is OAuth2-branded
// client-credentials WITHOUT a grant_type param. MIT/free. See human/setup/action1.md.
package action1

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/fleetdm/fleet/v4/pkg/fleethttp"
	"github.com/fleetdm/fleet/v4/server/community"
	"github.com/fleetdm/fleet/v4/server/contexts/ctxerr"
	"github.com/fleetdm/fleet/v4/server/fleet"
)

const (
	defaultStaleAfter = 7 * 24 * time.Hour
	// maxResponseBytes caps how much of a vendor response body we read (io.LimitReader) so a
	// malicious or spoofed API cannot OOM the collector.
	maxResponseBytes = 32 << 20 // 32 MB
	// pageLimit is the per-page item count requested from Action1 list endpoints.
	pageLimit = 500
	// maxPages caps pagination so a vendor that keeps returning next_page cannot loop us forever.
	maxPages = 1000
)

// Config is the per-tenant Action1 configuration. ClientSecret must be stored envelope-encrypted
// (human/RISK-REGISTER.md #3), never plaintext.
type Config struct {
	// BaseURL is the regional API base including the version path, e.g. "https://app.action1.com/api/3.0"
	// (NA), "https://app.eu.action1.com/api/3.0" (EU). A wrong region fails auth.
	BaseURL string
	// ClientID / ClientSecret are the API credentials (Configuration → Users & API Credentials).
	ClientID     string
	ClientSecret string
	// OrgID scopes all data calls (endpoints/updates are org-scoped).
	OrgID string
	// AgentDownloadID is the org-specific agent-download UUID for Layer A (the "<id>" in the MSI URL). It
	// is NOT the same as OrgID — store it separately (see human/setup/action1.md).
	AgentDownloadID string
	// StaleAfter is how long without a successful agent check-in before a host is reported at_risk
	// ("no successes recently"). Defaults to 7 days.
	StaleAfter time.Duration
}

// Provider implements community.HostStatusProvider and community.Collector for Action1.
type Provider struct {
	cfg Config

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// New returns an Action1 provider.
func New(cfg Config) *Provider {
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = defaultStaleAfter
	}
	return &Provider{cfg: cfg}
}

// Source implements community.HostStatusProvider.
func (p *Provider) Source() string { return "action1" }

// Categories implements community.HostStatusProvider: Action1 owns the "patching" coverage column.
func (p *Provider) Categories() []fleet.IntegrationCategory {
	return []fleet.IntegrationCategory{fleet.IntegrationCategoryPatching}
}

// AgentInstallURL returns the org-specific Windows agent MSI URL for Layer A deployment.
func (p *Provider) AgentInstallURL() string {
	return fmt.Sprintf("https://app.action1.com/agent/%s/Windows/agent.msi", url.PathEscape(p.cfg.AgentDownloadID))
}

// WindowsInstallScript returns a PowerShell script that downloads and silently installs the Action1 agent.
// Runs via the orbit scripts engine as SYSTEM (elevated, machine-wide — required; a non-elevated run
// silently fails), so it works on the free/community core without the software-installer pipeline.
func (p *Provider) WindowsInstallScript() string {
	return fmt.Sprintf(`$ErrorActionPreference = "Stop"
$url = "%s"
$msi = "$env:TEMP\action1_agent.msi"
Invoke-WebRequest -Uri $url -OutFile $msi -UseBasicParsing
& msiexec.exe /i $msi /quiet /qn /norestart
`, p.AgentInstallURL())
}

// Collect implements community.Collector: polls managed endpoints + missing updates and reports one
// "patching" reading per endpoint keyed by hostname. No DB access — the Runner resolves and persists.
// No-op if unconfigured.
func (p *Provider) Collect(ctx context.Context) ([]community.HostStatusReport, error) {
	if p.cfg.ClientID == "" || p.cfg.ClientSecret == "" || p.cfg.OrgID == "" {
		return nil, nil
	}
	endpoints, err := p.managedEndpoints(ctx)
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "action1 managed endpoints")
	}
	missing, err := p.missingUpdateCounts(ctx)
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "action1 missing updates")
	}
	reports := make([]community.HostStatusReport, 0, len(endpoints))
	for _, e := range endpoints {
		if e.Name == "" {
			continue
		}
		state, detail := patchState(e, missing[e.ID], p.cfg.StaleAfter)
		reports = append(reports, community.HostStatusReport{
			Identifier:     e.Name,
			IdentifierKind: community.IdentifierHostname,
			Category:       fleet.IntegrationCategoryPatching,
			State:          state,
			Detail:         detail,
		})
	}
	return reports, nil
}

// patchState maps an endpoint's agent freshness + missing-update count to a normalized patching state.
func patchState(e managedEndpoint, missingCount int, staleAfter time.Duration) (fleet.IntegrationState, string) {
	// A silent agent ("no successes recently") is the canonical at_risk we alert on.
	if e.LastSeen != nil && time.Since(*e.LastSeen) > staleAfter {
		return fleet.IntegrationStateAtRisk, fmt.Sprintf("no check-in in %s", time.Since(*e.LastSeen).Round(time.Hour))
	}
	if e.LastSeen == nil {
		return fleet.IntegrationStateAtRisk, "agent never reported"
	}
	if missingCount > 0 {
		return fleet.IntegrationStateAtRisk, fmt.Sprintf("%d updates missing", missingCount)
	}
	return fleet.IntegrationStateProtected, "up to date"
}

// --- REST plumbing ---

type managedEndpoint struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	LastSeen *time.Time `json:"last_seen"`
}

type missingUpdate struct {
	EndpointID string `json:"endpoint_id"`
}

func (p *Provider) managedEndpoints(ctx context.Context) ([]managedEndpoint, error) {
	return listAll[managedEndpoint](ctx, p, "/endpoints/managed/"+url.PathEscape(p.cfg.OrgID))
}

// missingUpdateCounts returns a count of missing updates per endpoint id.
func (p *Provider) missingUpdateCounts(ctx context.Context) (map[string]int, error) {
	items, err := listAll[missingUpdate](ctx, p, "/updates/"+url.PathEscape(p.cfg.OrgID))
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(items))
	for _, u := range items {
		counts[u.EndpointID]++
	}
	return counts, nil
}

// listAll pages through an Action1 list endpoint with from/limit offset paging, following the
// response's next_page cursor until the org is exhausted (capped at maxPages). A single unpaged
// read silently truncates orgs past the API page size, reporting false-green patching cells for
// hosts on later pages.
func listAll[T any](ctx context.Context, p *Provider, path string) ([]T, error) {
	var all []T
	from := 0
	for page := 0; page < maxPages; page++ {
		var out struct {
			Items    []T    `json:"items"`
			NextPage string `json:"next_page"`
		}
		if err := p.get(ctx, fmt.Sprintf("%s?from=%d&limit=%d", path, from, pageLimit), &out); err != nil {
			return nil, err
		}
		all = append(all, out.Items...)
		if out.NextPage == "" || len(out.Items) == 0 {
			return all, nil
		}
		from += len(out.Items)
	}
	return nil, ctxerr.Errorf(ctx, "action1 GET %s not exhausted after %d pages", path, maxPages)
}

// get performs an authenticated GET and decodes the JSON body into out.
func (p *Provider) get(ctx context.Context, path string, out any) error {
	token, err := p.getToken(ctx)
	if err != nil {
		return ctxerr.Wrap(ctx, err, "get token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(p.cfg.BaseURL, "/")+path, nil)
	if err != nil {
		return ctxerr.Wrap(ctx, err, "build request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	client := fleethttp.NewClient(fleethttp.WithTimeout(30 * time.Second))
	resp, err := client.Do(req)
	if err != nil {
		return ctxerr.Wrap(ctx, err, "do request")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ctxerr.Errorf(ctx, "action1 GET %s status %d", path, resp.StatusCode)
	}
	// io.LimitReader semantics with one sentinel byte past maxResponseBytes: if the decoder drains
	// the reader (N reaches 0), the body exceeded the cap.
	lr := &io.LimitedReader{R: resp.Body, N: maxResponseBytes + 1}
	if err := json.NewDecoder(lr).Decode(out); err != nil {
		return ctxerr.Wrap(ctx, err, "decode response")
	}
	if lr.N <= 0 {
		return ctxerr.Errorf(ctx, "action1 GET %s response exceeds %d bytes", path, maxResponseBytes)
	}
	return nil
}

// getToken returns a cached bearer token, refreshing via client-credentials when near expiry. Action1's
// token endpoint takes client_id/client_secret as form fields with NO grant_type param — a generic OAuth2
// client library fails here.
func (p *Provider) getToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.token != "" && time.Until(p.tokenExp) > 5*time.Second {
		return p.token, nil
	}

	form := url.Values{}
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(p.cfg.BaseURL, "/")+"/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", ctxerr.Wrap(ctx, err, "build token request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := fleethttp.NewClient(fleethttp.WithTimeout(30 * time.Second))
	resp, err := client.Do(req)
	if err != nil {
		return "", ctxerr.Wrap(ctx, err, "do token request")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ctxerr.Errorf(ctx, "action1 token status %d", resp.StatusCode)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	lr := &io.LimitedReader{R: resp.Body, N: maxResponseBytes + 1}
	if err := json.NewDecoder(lr).Decode(&tok); err != nil {
		return "", ctxerr.Wrap(ctx, err, "decode token")
	}
	if lr.N <= 0 {
		return "", ctxerr.Errorf(ctx, "action1 token response exceeds %d bytes", maxResponseBytes)
	}
	if tok.AccessToken == "" {
		return "", ctxerr.New(ctx, "action1 returned empty access token")
	}
	p.token = tok.AccessToken
	p.tokenExp = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	return p.token, nil
}
