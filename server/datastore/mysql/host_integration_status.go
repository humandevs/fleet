package mysql

import (
	"context"
	"strings"

	"github.com/fleetdm/fleet/v4/server/contexts/ctxerr"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/jmoiron/sqlx"
)

// coverageFreshExpr is a constant SQL boolean that is true when a host_integration_status row `s` is still
// fresh for its category. TTLs (seconds) mirror the read-path staleness gate (applyIntegrationStaleness):
// av/mdr 2h, remote_access 1h, backups 36h, disk_encryption/patching 24h, else 2h. A row failing this is
// stale ⇒ treated as "unknown"/uncovered by the coverage filters, so a stale "protected" never hides a gap.
const coverageFreshExpr = "s.updated_at > (NOW(6) - INTERVAL (CASE s.category " +
	"WHEN 'av' THEN 7200 WHEN 'mdr' THEN 7200 WHEN 'remote_access' THEN 3600 " +
	"WHEN 'backups' THEN 129600 WHEN 'disk_encryption' THEN 86400 WHEN 'patching' THEN 86400 " +
	"ELSE 7200 END) SECOND)"

// coverageFilterConds builds the parameterized WHERE conditions (against a host alias `h`) for a
// CoverageFilter. All row values are placeholders in args; the only inlined SQL is the constant freshness
// expression. Pure and unit-testable (no DB). Returns nil conds for a zero filter.
func coverageFilterConds(f fleet.CoverageFilter) (conds []string, args []any) {
	if f.Problems {
		// Any effectively-non-protected cell: wrong state OR gone stale.
		conds = append(conds, "EXISTS (SELECT 1 FROM host_integration_status s WHERE s.host_id = h.id "+
			"AND (s.state <> 'protected' OR NOT ("+coverageFreshExpr+")))")
	}
	for _, cat := range f.MissingCategories {
		// No fresh protected cell for this category.
		conds = append(conds, "NOT EXISTS (SELECT 1 FROM host_integration_status s WHERE s.host_id = h.id "+
			"AND s.category = ? AND s.state = 'protected' AND "+coverageFreshExpr+")")
		args = append(args, string(cat))
	}
	for _, p := range f.StatePredicates {
		if p.State == fleet.IntegrationStateUnknown {
			// "unknown" also captures cells that are stored-unknown OR stale past TTL.
			conds = append(conds, "EXISTS (SELECT 1 FROM host_integration_status s WHERE s.host_id = h.id "+
				"AND s.category = ? AND (s.state = 'unknown' OR NOT ("+coverageFreshExpr+")))")
			args = append(args, string(p.Category))
			continue
		}
		conds = append(conds, "EXISTS (SELECT 1 FROM host_integration_status s WHERE s.host_id = h.id "+
			"AND s.category = ? AND s.state = ? AND "+coverageFreshExpr+")")
		args = append(args, string(p.Category), string(p.State))
	}
	return conds, args
}

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

// ListHostsByCoverage returns the IDs of hosts matching the coverage filter (N-able-style views). Returns
// nil for a zero filter. It selects IDs only; the endpoint layer hydrates + paginates via the standard
// host list (see human/RFC-coverage-dashboards-and-bundles.md §6). Not yet on the fleet.Datastore
// interface — promoted when the host-list endpoint is wired with the frontend.
func (ds *Datastore) ListHostsByCoverage(ctx context.Context, f fleet.CoverageFilter) ([]uint, error) {
	conds, args := coverageFilterConds(f)
	if len(conds) == 0 {
		return nil, nil
	}
	stmt := "SELECT h.id FROM hosts h WHERE " + strings.Join(conds, " AND ") + " ORDER BY h.id"
	var ids []uint
	if err := sqlx.SelectContext(ctx, ds.reader(ctx), &ids, stmt, args...); err != nil {
		return nil, ctxerr.Wrap(ctx, err, "list hosts by coverage")
	}
	return ids, nil
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
