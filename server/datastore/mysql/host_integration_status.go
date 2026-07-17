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
		// A problem host either has no coverage cells at all (never reported by any provider — the
		// N-able "no data ⇒ RED" rule) or has any effectively-non-protected cell (wrong state OR gone
		// stale past its TTL).
		conds = append(conds, "(NOT EXISTS (SELECT 1 FROM host_integration_status s WHERE s.host_id = h.id) "+
			"OR EXISTS (SELECT 1 FROM host_integration_status s WHERE s.host_id = h.id "+
			"AND (s.state <> 'protected' OR NOT ("+coverageFreshExpr+"))))")
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

// filterHostsByCoverage appends the community coverage conditions to a host-list query (see
// applyHostFilters), reusing coverageFilterConds so the "problem devices" host list matches the coverage
// matrix exactly. It is a no-op for a zero CoverageFilter, and is safe to chain alongside the other
// filterHostsBy* helpers: it only appends AND-conditions (against host alias `h`) plus their params, before
// ORDER BY / LIMIT are added.
func filterHostsByCoverage(sql string, opt fleet.HostListOptions, params []interface{}) (string, []interface{}) {
	if opt.CoverageFilter.IsZero() {
		return sql, params
	}
	conds, args := coverageFilterConds(opt.CoverageFilter)
	for _, c := range conds {
		sql += " AND " + c
	}
	return sql, append(params, args...)
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

// coverageEffectiveStateExpr renders a cell's effective state for rollups: a row past its category
// TTL counts as "unknown", so dashboard tiles never count stale data as covered (the same invariant
// as applyIntegrationStaleness and coverageFilterConds).
const coverageEffectiveStateExpr = "IF(" + coverageFreshExpr + ", s.state, 'unknown')"

// buildAggregatedIntegrationStatusStmt assembles the rollup statement from WHERE conditions
// (against host alias `h`); hoisted so the pure builder test can pin the staleness-aware grouping
// without a DB.
func buildAggregatedIntegrationStatusStmt(conds []string) string {
	return `
SELECT s.source, s.category, ` + coverageEffectiveStateExpr + ` AS state, COUNT(*) AS count
FROM host_integration_status s
JOIN hosts h ON h.id = s.host_id
WHERE ` + strings.Join(conds, " AND ") + `
GROUP BY s.source, s.category, ` + coverageEffectiveStateExpr + `
ORDER BY s.source, s.category, state
`
}

// AggregatedHostIntegrationStatus returns a rollup of coverage cells grouped by (source, category,
// effective state), where a cell past its freshness TTL is counted as "unknown" rather than its
// last-written state. The rollup is always viewer-scoped via the team filter (a team-scoped user
// only ever sees their own teams' cells), and optionally restricted to one team — teamID 0 means
// hosts with no team, matching the team_id API convention.
func (ds *Datastore) AggregatedHostIntegrationStatus(ctx context.Context, filter fleet.TeamFilter, teamID *uint) ([]*fleet.AggregatedIntegrationStatus, error) {
	conds := []string{ds.whereFilterHostsByTeams(filter, "h")}
	var args []any
	if teamID != nil {
		if *teamID == 0 {
			conds = append(conds, "h.team_id IS NULL")
		} else {
			conds = append(conds, "h.team_id = ?")
			args = append(args, *teamID)
		}
	}
	var rows []*fleet.AggregatedIntegrationStatus
	if err := sqlx.SelectContext(ctx, ds.reader(ctx), &rows, buildAggregatedIntegrationStatusStmt(conds), args...); err != nil {
		return nil, ctxerr.Wrap(ctx, err, "aggregated host integration status")
	}
	return rows, nil
}
