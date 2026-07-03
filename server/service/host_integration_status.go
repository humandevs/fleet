package service

import (
	"context"
	"time"

	"github.com/fleetdm/fleet/v4/server/contexts/ctxerr"
	"github.com/fleetdm/fleet/v4/server/fleet"
)

////////////////////////////////////////////////////////////////////////////////
// Host integration status (community-plugin coverage matrix)
////////////////////////////////////////////////////////////////////////////////

// integrationStaleTTL is the per-category freshness window. A cell older than its TTL is rendered
// "unknown" (Stale=true) so the coverage matrix never shows stale data as covered — see
// human/RISK-REGISTER.md decide-now #3.
var integrationStaleTTL = map[fleet.IntegrationCategory]time.Duration{
	fleet.IntegrationCategoryAV:             2 * time.Hour,
	fleet.IntegrationCategoryMDR:            2 * time.Hour,
	fleet.IntegrationCategoryRemoteAccess:   1 * time.Hour,
	fleet.IntegrationCategoryBackups:        36 * time.Hour, // backups typically run daily
	fleet.IntegrationCategoryDiskEncryption: 24 * time.Hour,
}

const defaultIntegrationStaleTTL = 2 * time.Hour

type getHostIntegrationStatusRequest struct {
	ID uint `url:"id"`
}

type getHostIntegrationStatusResponse struct {
	Err               error                          `json:"error,omitempty"`
	IntegrationStatus []*fleet.HostIntegrationStatus `json:"integration_status"`
}

func (r getHostIntegrationStatusResponse) Error() error { return r.Err }

func getHostIntegrationStatusEndpoint(ctx context.Context, request interface{}, svc fleet.Service) (fleet.Errorer, error) {
	req := request.(*getHostIntegrationStatusRequest)
	statuses, err := svc.HostIntegrationStatus(ctx, req.ID)
	if err != nil {
		return getHostIntegrationStatusResponse{Err: err}, nil
	}
	return getHostIntegrationStatusResponse{IntegrationStatus: statuses}, nil
}

func (svc *Service) HostIntegrationStatus(ctx context.Context, hostID uint) ([]*fleet.HostIntegrationStatus, error) {
	if err := svc.authz.Authorize(ctx, &fleet.Host{}, fleet.ActionList); err != nil {
		return nil, err
	}

	host, err := svc.ds.HostLite(ctx, hostID)
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "find host for integration status")
	}

	if err := svc.authz.Authorize(ctx, host, fleet.ActionRead); err != nil {
		return nil, err
	}

	statuses, err := svc.ds.ListHostIntegrationStatus(ctx, hostID)
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "list host integration status")
	}

	// Freshness gate: a cell older than its category TTL is rendered unknown so the coverage matrix
	// never shows stale data as covered.
	now := time.Now()
	for _, s := range statuses {
		ttl := defaultIntegrationStaleTTL
		if t, ok := integrationStaleTTL[s.Category]; ok {
			ttl = t
		}
		if now.Sub(s.UpdatedAt) > ttl {
			s.Stale = true
			s.State = fleet.IntegrationStateUnknown
		}
	}

	return statuses, nil
}
