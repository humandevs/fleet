package mysql

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/fleetdm/fleet/v4/server/contexts/ctxerr"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/jmoiron/sqlx"
)

// coverageFreshExpr is a SQL boolean that is true when a host_integration_status row `s` is still fresh
// for its category. A row failing this is stale ⇒ treated as "unknown"/uncovered by the coverage filters
// and rollups, so a stale "protected" never hides a gap. It is DERIVED from fleet.IntegrationStaleTTL (the
// single source of truth), so the SQL freshness gate can never drift from the read-path staleness gate
// (service applyIntegrationStaleness).
var coverageFreshExpr = buildCoverageFreshExpr()

// buildCoverageFreshExpr renders the freshness CASE from fleet.IntegrationStaleTTL. The category names
// are our own enum constants (never user input), so inlining them as SQL literals is safe; categories
// are sorted so the generated expression is deterministic (stable across builds and testable).
func buildCoverageFreshExpr() string {
	cats := make([]string, 0, len(fleet.IntegrationStaleTTL))
	for c := range fleet.IntegrationStaleTTL {
		cats = append(cats, string(c))
	}
	sort.Strings(cats)
	expr := "s.updated_at > (NOW(6) - INTERVAL (CASE s.category"
	for _, c := range cats {
		secs := int64(fleet.IntegrationStaleTTL[fleet.IntegrationCategory(c)].Seconds())
		expr += " WHEN '" + c + "' THEN " + strconv.FormatInt(secs, 10)
	}
	expr += " ELSE " + strconv.FormatInt(int64(fleet.DefaultIntegrationStaleTTL.Seconds()), 10) + " END) SECOND)"
	return expr
}

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

// coverageEffectiveStateExpr renders a cell's effective state for rollups: a row past its category
// TTL counts as "unknown", so dashboard tiles never count stale data as covered (the same invariant
// as applyIntegrationStaleness and coverageFilterConds). Derived from coverageFreshExpr (a var), so it
// is a var too.
var coverageEffectiveStateExpr = "IF(" + coverageFreshExpr + ", s.state, 'unknown')"

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
