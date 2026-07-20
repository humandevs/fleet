// Package bitdefender is the community Bitdefender GravityZone (AV/EDR) provider. Bitdefender's old
// direct-integration SDK is deprecated; the supported surface is the GravityZone Control Center Public
// API — JSON-RPC 2.0 over HTTPS with HTTP Basic auth (API key as username, empty password). This is a
// hand-rolled thin client (a handful of POSTs), per human/setup/bitdefender-gravityzone.md. It reports
// the "av" and "mdr" coverage columns. MIT/free — no enterprise license, coexists with it.
package bitdefender

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/fleetdm/fleet/v4/pkg/fleethttp"
	"github.com/fleetdm/fleet/v4/server/community"
	"github.com/fleetdm/fleet/v4/server/contexts/ctxerr"
	"github.com/fleetdm/fleet/v4/server/fleet"
)

const defaultPageSize = 100 // GravityZone getEndpointsList perPage max

// maxPages caps how many inventory pages we will follow: the vendor-reported pagesCount is untrusted
// input, and a malicious or buggy Control Center must not be able to keep the collector paging forever.
const maxPages = 1000

// maxEndpoints caps the accumulated inventory size (1000 pages × 100 per page) so a spoofed console
// can't balloon the collector's memory with an unbounded endpoint list.
const maxEndpoints = 100000

// maxResponseBytes caps how much of a JSON-RPC response body we read (32 MB — far above any real
// GravityZone reply): a malicious or spoofed Control Center must not be able to OOM the collector with
// an unbounded body.
const maxResponseBytes = 32 << 20

// Config is the per-tenant GravityZone configuration. APIKey must be stored envelope-encrypted
// (human/RISK-REGISTER.md #3), never plaintext.
type Config struct {
	// Host is the Control Center host ONLY (no path), e.g. "https://cloud.gravityzone.bitdefender.com"
	// (US/default), "https://cloudgz.gravityzone.bitdefender.com" (EU), or your on-prem console. The
	// "/api/v1.0/jsonrpc/<service>" suffix is appended in code so we never double "/api". A wrong region
	// silently 401s.
	Host string
	// APIKey is the Control Center API key; it IS the credential (no OAuth). Scoped at creation to the
	// APIs it may call (needs Network).
	APIKey string
	// PageSize overrides the endpoints page size (default/max 100).
	PageSize int
}

// Provider implements community.HostStatusProvider and community.Collector for Bitdefender GravityZone.
type Provider struct {
	cfg    Config
	logger *slog.Logger
}

// New returns a GravityZone provider.
func New(cfg Config) *Provider {
	if cfg.PageSize <= 0 || cfg.PageSize > defaultPageSize {
		cfg.PageSize = defaultPageSize
	}
	return &Provider{cfg: cfg, logger: slog.Default()}
}

// Source implements community.HostStatusProvider.
func (p *Provider) Source() string { return "bitdefender" }

// Categories implements community.HostStatusProvider: GravityZone covers antivirus and, where the EDR
// sensor is licensed, managed detection & response.
func (p *Provider) Categories() []fleet.IntegrationCategory {
	return []fleet.IntegrationCategory{fleet.IntegrationCategoryAV, fleet.IntegrationCategoryMDR}
}

// Collect implements community.Collector: pages the managed endpoint inventory, pulls per-endpoint
// details, and maps each to an "av" and an "mdr" reading keyed by the endpoint FQDN/hostname. No DB
// access — the community Runner resolves hostnames to hosts and persists. No-op if unconfigured.
func (p *Provider) Collect(ctx context.Context) ([]community.HostStatusReport, error) {
	if p.cfg.Host == "" || p.cfg.APIKey == "" {
		return nil, nil
	}
	endpoints, err := p.listEndpoints(ctx)
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "bitdefender list endpoints")
	}
	reports := make([]community.HostStatusReport, 0, len(endpoints)*2)
	for _, e := range endpoints {
		det, err := p.endpointDetails(ctx, e.ID)
		if err != nil {
			// One bad endpoint must not blank the whole fleet's av/mdr columns — log and move on so the
			// rest of the sweep still reports.
			p.logger.WarnContext(ctx, "bitdefender endpoint details failed, skipping endpoint",
				"endpoint_id", e.ID, "err", err)
			continue
		}
		name := det.Name
		if name == "" {
			name = e.Name
		}
		if name == "" {
			continue
		}
		avState, avDetail, mdrState, mdrDetail := mapStates(det)
		reports = append(reports,
			community.HostStatusReport{Identifier: name, IdentifierKind: community.IdentifierHostname, Category: fleet.IntegrationCategoryAV, State: avState, Detail: avDetail},
			community.HostStatusReport{Identifier: name, IdentifierKind: community.IdentifierHostname, Category: fleet.IntegrationCategoryMDR, State: mdrState, Detail: mdrDetail},
		)
	}
	return reports, nil
}

// mapStates derives the normalized av/mdr coverage from endpoint details. Conservative mapping; exact
// GravityZone field names are confirmed against a live tenant per the setup doc.
func mapStates(d endpointDetails) (avState fleet.IntegrationState, avDetail string, mdrState fleet.IntegrationState, mdrDetail string) {
	switch {
	case !d.Modules.Antimalware:
		avState, avDetail = fleet.IntegrationStateNotInstalled, "antimalware module off"
	case d.MalwareStatus.Infected || d.MalwareStatus.Detection:
		avState, avDetail = fleet.IntegrationStateAtRisk, "active malware detection"
	case d.Agent.ProductOutdated:
		avState, avDetail = fleet.IntegrationStateAtRisk, "agent outdated"
	default:
		avState, avDetail = fleet.IntegrationStateProtected, "protected"
	}

	// EDR/MDR is covered when the EDR sensor or advanced threat control module is on.
	if d.Modules.EDRSensor || d.Modules.AdvancedThreatControl {
		mdrState, mdrDetail = fleet.IntegrationStateProtected, "edr sensor active"
	} else {
		mdrState, mdrDetail = fleet.IntegrationStateNotInstalled, "no edr sensor"
	}
	return avState, avDetail, mdrState, mdrDetail
}

// --- JSON-RPC plumbing ---

type endpoint struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type endpointsPage struct {
	Page       int        `json:"page"`
	PagesCount int        `json:"pagesCount"`
	Total      int        `json:"total"`
	Items      []endpoint `json:"items"`
}

// endpointDetails is the subset of getManagedEndpointDetails we consume. Field names per the setup doc's
// documented model; parsed defensively (unknown fields ignored).
type endpointDetails struct {
	Name  string `json:"name"`
	Agent struct {
		ProductOutdated bool `json:"productOutdated"`
	} `json:"agent"`
	MalwareStatus struct {
		Detection bool `json:"detection"`
		Infected  bool `json:"infected"`
	} `json:"malwareStatus"`
	Modules struct {
		Antimalware           bool `json:"antimalware"`
		AdvancedThreatControl bool `json:"advancedThreatControl"`
		EDRSensor             bool `json:"edrSensor"`
	} `json:"modules"`
}

func (p *Provider) listEndpoints(ctx context.Context) ([]endpoint, error) {
	var all []endpoint
	for page := 1; ; page++ {
		// pagesCount comes from the vendor and is untrusted — cap the loop and the accumulated inventory
		// rather than paging (and allocating) forever. Erroring beats silently truncating: a partial sweep
		// reported as complete would flip the missing endpoints' columns to stale/unknown.
		if page > maxPages {
			return nil, ctxerr.Errorf(ctx, "gravityzone endpoint inventory exceeds %d pages", maxPages)
		}
		var out endpointsPage
		params := map[string]any{"page": page, "perPage": p.cfg.PageSize}
		if err := p.call(ctx, "network", "getEndpointsList", params, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Items...)
		if len(all) > maxEndpoints {
			return nil, ctxerr.Errorf(ctx, "gravityzone endpoint inventory exceeds %d endpoints", maxEndpoints)
		}
		if page >= out.PagesCount || len(out.Items) == 0 {
			break
		}
	}
	return all, nil
}

func (p *Provider) endpointDetails(ctx context.Context, id string) (endpointDetails, error) {
	var out endpointDetails
	err := p.call(ctx, "network", "getManagedEndpointDetails", map[string]any{"endpointId": id}, &out)
	return out, err
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
	ID      int    `json:"id"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// call POSTs a JSON-RPC 2.0 request to the given service and unmarshals result into out. GravityZone
// returns HTTP 200 even on protocol errors, so the body's "error" field is inspected, not just status.
func (p *Provider) call(ctx context.Context, service, method string, params any, out any) error {
	endpoint := strings.TrimRight(p.cfg.Host, "/") + "/api/v1.0/jsonrpc/" + service
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params, ID: 1})
	if err != nil {
		return ctxerr.Wrap(ctx, err, "marshal rpc request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return ctxerr.Wrap(ctx, err, "build request")
	}
	// Basic auth: username = API key, password = empty.
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(p.cfg.APIKey+":")))
	req.Header.Set("Content-Type", "application/json")

	client := fleethttp.NewClient(fleethttp.WithTimeout(30 * time.Second))
	resp, err := client.Do(req)
	if err != nil {
		return ctxerr.Wrap(ctx, err, "do request")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ctxerr.Errorf(ctx, "gravityzone %s.%s http status %d", service, method, resp.StatusCode)
	}
	// Read at most maxResponseBytes+1 so an over-limit body is distinguishable from one exactly at the
	// limit, and fail loudly rather than decode a truncated envelope.
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return ctxerr.Wrap(ctx, err, "read response body")
	}
	if len(respBody) > maxResponseBytes {
		return ctxerr.Errorf(ctx, "gravityzone %s.%s response exceeds %d bytes", service, method, maxResponseBytes)
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return ctxerr.Wrap(ctx, err, "decode rpc envelope")
	}
	if envelope.Error != nil {
		return ctxerr.Errorf(ctx, "gravityzone %s.%s rpc error %d: %s", service, method, envelope.Error.Code, envelope.Error.Message)
	}
	if err := json.Unmarshal(envelope.Result, out); err != nil {
		return ctxerr.Wrap(ctx, err, "decode rpc result")
	}
	return nil
}
