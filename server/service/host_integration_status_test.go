package service

import (
	"testing"
	"time"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/stretchr/testify/require"
)

func TestCoverageFilterFromRequest(t *testing.T) {
	// problems flag
	f := coverageFilterFromRequest(&getHostsByCoverageRequest{Problems: true})
	require.True(t, f.Problems)
	require.Empty(t, f.MissingCategories)

	// missing categories (comma-separated, trimmed)
	f = coverageFilterFromRequest(&getHostsByCoverageRequest{Missing: "av, mdr ,patching"})
	require.ElementsMatch(t,
		[]fleet.IntegrationCategory{"av", "mdr", "patching"},
		f.MissingCategories,
	)

	// exact predicate needs both category and state
	f = coverageFilterFromRequest(&getHostsByCoverageRequest{Category: "backups", State: "at_risk"})
	require.Len(t, f.StatePredicates, 1)
	require.Equal(t, fleet.IntegrationCategory("backups"), f.StatePredicates[0].Category)
	require.Equal(t, fleet.IntegrationState("at_risk"), f.StatePredicates[0].State)

	// category without state is ignored (no partial predicate)
	f = coverageFilterFromRequest(&getHostsByCoverageRequest{Category: "backups"})
	require.Empty(t, f.StatePredicates)

	// empty request → zero filter
	require.True(t, coverageFilterFromRequest(&getHostsByCoverageRequest{}).IsZero())
}

func TestApplyIntegrationStaleness(t *testing.T) {
	now := time.Now()
	statuses := []*fleet.HostIntegrationStatus{
		// Fresh AV (TTL 2h) — stays protected.
		{Category: fleet.IntegrationCategoryAV, State: fleet.IntegrationStateProtected, UpdatedAt: now.Add(-30 * time.Minute)},
		// Stale AV (older than 2h) — forced unknown + stale.
		{Category: fleet.IntegrationCategoryAV, State: fleet.IntegrationStateProtected, UpdatedAt: now.Add(-3 * time.Hour)},
		// Backups within the 36h TTL — stays protected (daily cadence).
		{Category: fleet.IntegrationCategoryBackups, State: fleet.IntegrationStateProtected, UpdatedAt: now.Add(-24 * time.Hour)},
		// Unknown category falls back to the default TTL (2h); 3h old -> stale.
		{Category: fleet.IntegrationCategory("custom"), State: fleet.IntegrationStateAtRisk, UpdatedAt: now.Add(-3 * time.Hour)},
	}

	applyIntegrationStaleness(statuses, now)

	require.False(t, statuses[0].Stale)
	require.Equal(t, fleet.IntegrationStateProtected, statuses[0].State)

	require.True(t, statuses[1].Stale, "AV older than 2h should be stale")
	require.Equal(t, fleet.IntegrationStateUnknown, statuses[1].State)

	require.False(t, statuses[2].Stale, "backups within 36h should not be stale")
	require.Equal(t, fleet.IntegrationStateProtected, statuses[2].State)

	require.True(t, statuses[3].Stale, "unknown category should use the default TTL")
	require.Equal(t, fleet.IntegrationStateUnknown, statuses[3].State)
}
