package service

import (
	"context"
	"strings"
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
	fleet.IntegrationCategoryPatching:       24 * time.Hour, // must match coverageFreshExpr in the datastore
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

	applyIntegrationStaleness(statuses, time.Now())

	return statuses, nil
}

////////////////////////////////////////////////////////////////////////////////
// Hosts by coverage (N-able-style filter views)
////////////////////////////////////////////////////////////////////////////////

// getHostsByCoverageRequest decodes the coverage filter from query params:
//
//	?problems=true                         -> any effectively-non-protected cell
//	?missing=av,mdr                        -> missing a fresh protected cell in each listed category
//	?category=patching&state=at_risk       -> an exact (category,state) predicate
type getHostsByCoverageRequest struct {
	Problems bool   `query:"problems,optional"`
	Missing  string `query:"missing,optional"`  // comma-separated categories
	Category string `query:"category,optional"` // used with State for an exact predicate
	State    string `query:"state,optional"`
}

type getHostsByCoverageResponse struct {
	Err     error  `json:"error,omitempty"`
	Count   int    `json:"count"`
	HostIDs []uint `json:"host_ids"`
}

func (r getHostsByCoverageResponse) Error() error { return r.Err }

func getHostsByCoverageEndpoint(ctx context.Context, request interface{}, svc fleet.Service) (fleet.Errorer, error) {
	req := request.(*getHostsByCoverageRequest)
	filter := coverageFilterFromRequest(req)
	ids, err := svc.HostsByCoverage(ctx, filter)
	if err != nil {
		return getHostsByCoverageResponse{Err: err}, nil
	}
	return getHostsByCoverageResponse{Count: len(ids), HostIDs: ids}, nil
}

// coverageFilterFromRequest maps decoded query params to a CoverageFilter.
func coverageFilterFromRequest(req *getHostsByCoverageRequest) fleet.CoverageFilter {
	filter := fleet.CoverageFilter{Problems: req.Problems}
	for _, c := range strings.Split(req.Missing, ",") {
		if c = strings.TrimSpace(c); c != "" {
			filter.MissingCategories = append(filter.MissingCategories, fleet.IntegrationCategory(c))
		}
	}
	if req.Category != "" && req.State != "" {
		filter.StatePredicates = append(filter.StatePredicates, fleet.CoverageStatePredicate{
			Category: fleet.IntegrationCategory(req.Category),
			State:    fleet.IntegrationState(req.State),
		})
	}
	return filter
}

// HostsByCoverage returns the IDs of hosts matching the coverage filter. It authorizes host listing (the
// same gate as the host list) and delegates the freshness-aware matching to the datastore.
func (svc *Service) HostsByCoverage(ctx context.Context, filter fleet.CoverageFilter) ([]uint, error) {
	if err := svc.authz.Authorize(ctx, &fleet.Host{}, fleet.ActionList); err != nil {
		return nil, err
	}
	if filter.IsZero() {
		return nil, &fleet.BadRequestError{Message: "no coverage filter provided (set problems, missing, or category+state)"}
	}
	ids, err := svc.ds.ListHostsByCoverage(ctx, filter)
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "list hosts by coverage")
	}
	return ids, nil
}

// applyIntegrationStaleness marks any coverage cell older than its category freshness TTL as stale
// and forces its state to "unknown", so the coverage matrix never renders stale data as covered
// (human/RISK-REGISTER.md decide-now #3).
func applyIntegrationStaleness(statuses []*fleet.HostIntegrationStatus, now time.Time) {
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
}
