package mysql

import (
	"context"

	"github.com/fleetdm/fleet/v4/server/contexts/ctxerr"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/jmoiron/sqlx"
)

// SetOrUpdateHostIntegrationStatus upserts one (host, source, category) coverage cell reported by a
// community-plugin provider. updated_at is refreshed on every write so staleness can be computed on
// read.
func (ds *Datastore) SetOrUpdateHostIntegrationStatus(ctx context.Context, status *fleet.HostIntegrationStatus) error {
	const stmt = `
INSERT INTO host_integration_status (host_id, source, category, state, detail)
VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  state = VALUES(state),
  detail = VALUES(detail),
  updated_at = CURRENT_TIMESTAMP(6)
`
	if _, err := ds.writer(ctx).ExecContext(ctx, stmt,
		status.HostID, status.Source, string(status.Category), string(status.State), status.Detail,
	); err != nil {
		return ctxerr.Wrap(ctx, err, "upsert host integration status")
	}
	return nil
}

// ListHostIntegrationStatus returns all coverage cells for a host. Staleness is applied at the
// service layer, not here.
func (ds *Datastore) ListHostIntegrationStatus(ctx context.Context, hostID uint) ([]*fleet.HostIntegrationStatus, error) {
	const stmt = `
SELECT host_id, source, category, state, detail, updated_at
FROM host_integration_status
WHERE host_id = ?
ORDER BY source, category
`
	var rows []*fleet.HostIntegrationStatus
	if err := sqlx.SelectContext(ctx, ds.reader(ctx), &rows, stmt, hostID); err != nil {
		return nil, ctxerr.Wrap(ctx, err, "list host integration status")
	}
	return rows, nil
}

// AggregatedHostIntegrationStatus returns a fleet-wide, optionally team-scoped, rollup of coverage
// cells grouped by (source, category, state).
func (ds *Datastore) AggregatedHostIntegrationStatus(ctx context.Context, teamID *uint) ([]*fleet.AggregatedIntegrationStatus, error) {
	const stmtAll = `
SELECT source, category, state, COUNT(*) AS count
FROM host_integration_status
GROUP BY source, category, state
ORDER BY source, category, state
`
	const stmtTeam = `
SELECT his.source, his.category, his.state, COUNT(*) AS count
FROM host_integration_status his
JOIN hosts h ON h.id = his.host_id
WHERE h.team_id = ?
GROUP BY his.source, his.category, his.state
ORDER BY his.source, his.category, his.state
`
	var rows []*fleet.AggregatedIntegrationStatus
	var err error
	if teamID != nil {
		err = sqlx.SelectContext(ctx, ds.reader(ctx), &rows, stmtTeam, *teamID)
	} else {
		err = sqlx.SelectContext(ctx, ds.reader(ctx), &rows, stmtAll)
	}
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "aggregated host integration status")
	}
	return rows, nil
}
