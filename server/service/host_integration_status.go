package service

import (
	"context"
	"time"

	"github.com/fleetdm/fleet/v4/server/contexts/ctxerr"
	"github.com/fleetdm/fleet/v4/server/contexts/viewer"
	"github.com/fleetdm/fleet/v4/server/fleet"
)

////////////////////////////////////////////////////////////////////////////////
// Host integration status (community-plugin coverage matrix)
////////////////////////////////////////////////////////////////////////////////

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

	applyIntegrationStaleness(statuses, time.Now())

	return statuses, nil
}

////////////////////////////////////////////////////////////////////////////////
// Integration status summary (coverage dashboard tiles)
////////////////////////////////////////////////////////////////////////////////

type getIntegrationStatusSummaryRequest struct {
	TeamID *uint `query:"team_id,optional" renameto:"fleet_id"`
}

type getIntegrationStatusSummaryResponse struct {
	Err     error                                `json:"error,omitempty"`
	Summary []*fleet.AggregatedIntegrationStatus `json:"integration_status_summary"`
}

func (r getIntegrationStatusSummaryResponse) Error() error { return r.Err }

func getIntegrationStatusSummaryEndpoint(ctx context.Context, request interface{}, svc fleet.Service) (fleet.Errorer, error) {
	req := request.(*getIntegrationStatusSummaryRequest)
	summary, err := svc.AggregatedHostIntegrationStatus(ctx, req.TeamID)
	if err != nil {
		return getIntegrationStatusSummaryResponse{Err: err}, nil
	}
	return getIntegrationStatusSummaryResponse{Summary: summary}, nil
}

// AggregatedHostIntegrationStatus returns the coverage rollup for the dashboard tiles. Authorization
// mirrors the host summary: the coarse host-list gate plus a viewer team filter applied in SQL, so a
// team-scoped user only ever sees their own teams' rollup — with or without a team_id, and never
// another team's (the filter turns a non-member's team restriction into an empty result). Staleness
// is applied in SQL (a cell past its TTL counts as "unknown"), so the tiles agree with the coverage
// filters.
func (svc *Service) AggregatedHostIntegrationStatus(ctx context.Context, teamID *uint) ([]*fleet.AggregatedIntegrationStatus, error) {
	if err := svc.authz.Authorize(ctx, &fleet.Host{TeamID: teamID}, fleet.ActionList); err != nil {
		return nil, err
	}
	vc, ok := viewer.FromContext(ctx)
	if !ok {
		return nil, fleet.ErrNoContext
	}
	// 404 for a nonexistent team, mirroring AggregatedMacadminsData. team_id 0 means hosts with no
	// team, so there is no team to look up.
	if teamID != nil && *teamID > 0 {
		if _, err := svc.ds.TeamLite(ctx, *teamID); err != nil {
			return nil, ctxerr.Wrap(ctx, err, "find team for integration status summary")
		}
	}
	filter := fleet.TeamFilter{User: vc.User, IncludeObserver: true}
	summary, err := svc.ds.AggregatedHostIntegrationStatus(ctx, filter, teamID)
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "aggregated host integration status")
	}
	return summary, nil
}

// applyIntegrationStaleness marks any coverage cell older than its category freshness TTL as stale
// and forces its state to "unknown", so the coverage matrix never renders stale data as covered
// (human/RISK-REGISTER.md decide-now #3).
func applyIntegrationStaleness(statuses []*fleet.HostIntegrationStatus, now time.Time) {
	for _, s := range statuses {
		ttl := fleet.DefaultIntegrationStaleTTL
		if t, ok := fleet.IntegrationStaleTTL[s.Category]; ok {
			ttl = t
		}
		// Inclusive boundary (>=): a cell exactly at its TTL age is stale. This matches the SQL gate
		// coverageFreshExpr (fresh iff updated_at strictly newer than NOW-ttl ⇒ age == ttl is stale) to the
		// microsecond, so the host-detail read path and the coverage filters/rollup can never disagree.
		if now.Sub(s.UpdatedAt) >= ttl {
			s.Stale = true
			s.State = fleet.IntegrationStateUnknown
		}
	}
}
