// Package splashtop is the community Splashtop (MSP/Enterprise) provider. It reports remote-access coverage
// into the host coverage matrix by polling the Splashtop API for the team's computers and their online
// status (online → protected; known-but-offline → at_risk). It is the MVP's SECOND remote-access provider
// alongside screenconnect — the backup remote tool for cutting over from a legacy RMM — and, unlike
// MeshCentral, needs no self-hosted server (Splashtop is SaaS). MIT/free: it does not require the
// enterprise license and coexists with it.
//
// Confidence note (matches the honest scoping of the other community providers): the Splashtop API surface
// varies by plan/version. BaseURL, the auth scheme, and ComputersPath are therefore CONFIGURED, not
// hardcoded — confirm them against your MSP tenant's API docs before relying on data (see
// human/setup/splashtop.md). The parser tolerates unknown/missing fields so a slightly different response
// shape degrades gracefully rather than crashing the runner.
package splashtop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fleetdm/fleet/v4/pkg/fleethttp"
	"github.com/fleetdm/fleet/v4/server/community"
	"github.com/fleetdm/fleet/v4/server/contexts/ctxerr"
	"github.com/fleetdm/fleet/v4/server/fleet"
)

const (
	defaultBaseURL       = "https://api.splashtop.com"
	defaultComputersPath = "/v1/computers"
)

// Config is the Splashtop MSP/Enterprise API configuration. APIKey must be stored envelope-encrypted
// (human/RISK-REGISTER.md #3), never plaintext.
type Config struct {
	// BaseURL is the Splashtop API base. Defaults to https://api.splashtop.com. Override for a regional or
	// on-prem gateway.
	BaseURL string
	// APIKey is the MSP/Enterprise API add-on credential. Sent as an Authorization: Bearer header by
	// default. Empty ⇒ Collect is a no-op (unconfigured), so the provider can register its coverage column
	// before credentials are provisioned. If your tenant authenticates with a `publicapikey` query param
	// instead of a bearer header, adjust request() — the difference is documented in human/setup/splashtop.md.
	APIKey string
	// ComputersPath is the API path (relative to BaseURL) of the list-computers method, which must return
	// each computer's name + online status. Defaults to /v1/computers. Configurable because the versioned
	// path differs across plans.
	ComputersPath string
}

// Provider implements community.HostStatusProvider and community.Collector for Splashtop.
type Provider struct {
	cfg Config
}

// New returns a Splashtop provider, applying defaults for BaseURL and ComputersPath.
func New(cfg Config) *Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.ComputersPath == "" {
		cfg.ComputersPath = defaultComputersPath
	}
	return &Provider{cfg: cfg}
}

// Source implements community.HostStatusProvider.
func (p *Provider) Source() string { return "splashtop" }

// Categories implements community.HostStatusProvider: Splashtop owns the "remote_access" coverage column
// (a second provider in that category alongside screenconnect — each writes its own (host, source) cell).
func (p *Provider) Categories() []fleet.IntegrationCategory {
	return []fleet.IntegrationCategory{fleet.IntegrationCategoryRemoteAccess}
}

// Collect implements community.Collector: polls the Splashtop computer list and maps each computer to a
// remote_access reading keyed by its name (the host's name, resolved by the Runner to a Fleet host). Online
// ⇒ protected; known-but-offline ⇒ at_risk. No-op (returns nil) when unconfigured (no APIKey). Performs no
// DB access — the Runner resolves names to hosts and persists.
func (p *Provider) Collect(ctx context.Context) ([]community.HostStatusReport, error) {
	if p.cfg.APIKey == "" {
		return nil, nil
	}
	computers, err := p.listComputers(ctx)
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "splashtop list computers")
	}
	reports := make([]community.HostStatusReport, 0, len(computers))
	for _, c := range computers {
		if c.Name == "" {
			continue // no host key to resolve against
		}
		state := fleet.IntegrationStateAtRisk
		detail := "offline"
		if c.LastOnline != nil {
			detail = fmt.Sprintf("offline, last seen %s ago", time.Since(*c.LastOnline).Round(time.Minute))
		}
		if c.Online {
			state = fleet.IntegrationStateProtected
			detail = "online"
		}
		reports = append(reports, community.HostStatusReport{
			Identifier:     c.Name,
			IdentifierKind: community.IdentifierHostname,
			Category:       fleet.IntegrationCategoryRemoteAccess,
			State:          state,
			Detail:         detail,
		})
	}
	return reports, nil
}

// --- REST plumbing ---

// computer is the subset of a Splashtop computer object we consume. Parsed defensively — unknown fields are
// ignored and missing ones tolerated (see the package confidence note).
type computer struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Online     bool       `json:"online"`
	LastOnline *time.Time `json:"last_online"`
}

// listComputers calls the configured computers method and returns the parsed computers. It accepts either a
// wrapped ({"computers":[...]} or {"items":[...]}) or a bare-array response, since the wrapper key varies by
// plan/version.
func (p *Provider) listComputers(ctx context.Context) ([]computer, error) {
	body, err := p.get(ctx, p.cfg.ComputersPath)
	if err != nil {
		return nil, err
	}
	// Dispatch on the top-level JSON shape: only a body that actually starts with '[' is parsed as a bare
	// array. An object body is decoded as a wrapper and returned even when its list is empty or null —
	// falling through to the bare-array parse on an empty wrapper would misreport a legitimately empty
	// tenant as a decode error.
	if bytes.HasPrefix(bytes.TrimLeft(body, " \t\r\n"), []byte("[")) {
		var bare []computer
		if err := json.Unmarshal(body, &bare); err != nil {
			return nil, ctxerr.Wrap(ctx, err, "decode computers")
		}
		return bare, nil
	}
	var wrapped struct {
		Computers []computer `json:"computers"`
		Items     []computer `json:"items"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, ctxerr.Wrap(ctx, err, "decode computers")
	}
	if wrapped.Computers != nil {
		return wrapped.Computers, nil
	}
	return wrapped.Items, nil
}

// maxResponseBytes caps how much of a response body we read (32 MB — far above any real computer list): a
// malicious or spoofed Splashtop endpoint must not be able to OOM the collector with an unbounded body.
const maxResponseBytes = 32 << 20

// get performs an authenticated GET and returns the raw body (so the caller can try multiple JSON shapes).
func (p *Provider) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(p.cfg.BaseURL, "/")+path, nil)
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "build request")
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	req.Header.Set("Accept", "application/json")

	client := fleethttp.NewClient(fleethttp.WithTimeout(30 * time.Second))
	resp, err := client.Do(req)
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "do request")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, ctxerr.Errorf(ctx, "splashtop GET %s status %d", path, resp.StatusCode)
	}
	// Read at most maxResponseBytes+1 so an over-limit body is distinguishable from one exactly at the
	// limit, and fail loudly rather than decode a truncated list.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "read response")
	}
	if len(body) > maxResponseBytes {
		return nil, ctxerr.Errorf(ctx, "splashtop GET %s response exceeds %d bytes", path, maxResponseBytes)
	}
	return body, nil
}
