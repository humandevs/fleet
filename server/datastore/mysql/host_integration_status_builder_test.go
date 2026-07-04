package mysql

import (
	"testing"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/stretchr/testify/require"
)

// TestCoverageFilterConds exercises the pure coverage-filter SQL builder (no DB required).
func TestCoverageFilterConds(t *testing.T) {
	// Zero filter → no conditions.
	conds, args := coverageFilterConds(fleet.CoverageFilter{})
	require.Empty(t, conds)
	require.Empty(t, args)

	// Problems → one EXISTS with the non-protected-or-stale clause; freshness is a constant (no args).
	conds, args = coverageFilterConds(fleet.CoverageFilter{Problems: true})
	require.Len(t, conds, 1)
	require.Contains(t, conds[0], "EXISTS")
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
