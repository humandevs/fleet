package mysql

import (
	"strconv"
	"strings"
	"testing"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/stretchr/testify/require"
)

// TestCoverageFreshExprDerivedFromTTLs pins the SQL freshness gate to the single source of truth
// (fleet.IntegrationStaleTTL): every category's WHEN clause must carry that category's exact seconds,
// and the ELSE must be the default. This makes it impossible for the SQL gate to silently drift from
// the read-path staleness gate (service applyIntegrationStaleness) — the two used to be hand-kept in
// sync by a comment. No DB required.
func TestCoverageFreshExprDerivedFromTTLs(t *testing.T) {
	expr := buildCoverageFreshExpr()
	for cat, ttl := range fleet.IntegrationStaleTTL {
		want := "WHEN '" + string(cat) + "' THEN " + strconv.FormatInt(int64(ttl.Seconds()), 10)
		require.Contains(t, expr, want, "freshness expr must carry %s's TTL from fleet.IntegrationStaleTTL", cat)
	}
	require.Contains(t, expr, "ELSE "+strconv.FormatInt(int64(fleet.DefaultIntegrationStaleTTL.Seconds()), 10))
	// All six known categories are covered (guards against a category being added to the enum but not
	// the TTL map, which would silently fall through to the default).
	require.Len(t, fleet.IntegrationStaleTTL, 6)
}

// TestCoverageFilterConds exercises the pure coverage-filter SQL builder (no DB required).
func TestCoverageFilterConds(t *testing.T) {
	// Zero filter → no conditions.
	conds, args := coverageFilterConds(fleet.CoverageFilter{})
	require.Empty(t, conds)
	require.Empty(t, args)

	// Problems → one condition matching hosts with NO cells at all (never reported ⇒ RED) or any
	// non-protected-or-stale cell; freshness is a constant (no args).
	conds, args = coverageFilterConds(fleet.CoverageFilter{Problems: true})
	require.Len(t, conds, 1)
	require.Contains(t, conds[0], "NOT EXISTS (SELECT 1 FROM host_integration_status s WHERE s.host_id = h.id)")
	require.Contains(t, conds[0], "s.state <> 'protected'")
	require.Contains(t, conds[0], "NOT (s.updated_at >")
	require.Empty(t, args)

	// Missing AV → NOT EXISTS a fresh protected cell; category is parameterized.
	conds, args = coverageFilterConds(fleet.CoverageFilter{
		MissingCategories: []fleet.IntegrationCategory{fleet.IntegrationCategoryAV},
	})
	require.Len(t, conds, 1)
	require.Contains(t, conds[0], "NOT EXISTS")
	require.Contains(t, conds[0], "s.state = 'protected'")
	require.Equal(t, []any{"av"}, args)

	// Exact predicate → category + state placeholders, fresh-gated.
	conds, args = coverageFilterConds(fleet.CoverageFilter{
		StatePredicates: []fleet.CoverageStatePredicate{{Category: fleet.IntegrationCategoryPatching, State: fleet.IntegrationStateAtRisk}},
	})
	require.Len(t, conds, 1)
	require.Contains(t, conds[0], "s.category = ? AND s.state = ?")
	require.Equal(t, []any{"patching", "at_risk"}, args)

	// Unknown predicate → matches stored-unknown OR stale; only the category is an arg.
	conds, args = coverageFilterConds(fleet.CoverageFilter{
		StatePredicates: []fleet.CoverageStatePredicate{{Category: fleet.IntegrationCategoryAV, State: fleet.IntegrationStateUnknown}},
	})
	require.Contains(t, conds[0], "s.state = 'unknown' OR NOT")
	require.Equal(t, []any{"av"}, args)

	// Combined predicates accumulate conditions and args in order.
	conds, args = coverageFilterConds(fleet.CoverageFilter{
		Problems:          true,
		MissingCategories: []fleet.IntegrationCategory{fleet.IntegrationCategoryMDR},
	})
	require.Len(t, conds, 2)
	require.Equal(t, []any{"mdr"}, args)
}

// TestAggregatedIntegrationStatusStmtApplyStaleness pins the rollup statement to the effective-state
// grouping: a cell past its freshness TTL must be counted as "unknown", never as its last-written state,
// so the dashboard tiles agree with the coverage filters (no DB required).
func TestAggregatedIntegrationStatusStmtApplyStaleness(t *testing.T) {
	require.Contains(t, coverageEffectiveStateExpr, "s.updated_at >")
	require.Contains(t, coverageEffectiveStateExpr, "'unknown'")

	stmt := buildAggregatedIntegrationStatusStmt([]string{"TRUE", "h.team_id IS NULL"})
	// Both the SELECT and the GROUP BY must use the effective-state expression — grouping by the raw
	// column would silently count stale "protected" cells as covered.
	require.Equal(t, 2, strings.Count(stmt, coverageEffectiveStateExpr))
	require.Contains(t, stmt, "GROUP BY s.source, s.category, "+coverageEffectiveStateExpr)
	// WHERE conditions (viewer team filter + optional team restriction) are AND-combined.
	require.Contains(t, stmt, "WHERE TRUE AND h.team_id IS NULL")
}
