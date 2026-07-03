package service

import (
	"testing"
	"time"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/stretchr/testify/require"
)

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
