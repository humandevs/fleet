package service

import (
	"context"
	"testing"
	"time"

	"github.com/fleetdm/fleet/v4/server/contexts/viewer"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/fleetdm/fleet/v4/server/mock"
	"github.com/stretchr/testify/require"
)

// TestHostIntegrationStatusAuthz locks in the double-authorize on the per-host coverage endpoint: the
// coarse ActionList gate plus a per-host ActionRead on the LOADED host, so a team-scoped user can only
// read coverage cells for hosts on their own team. A regression that drops the second Authorize would
// let a team user read any host's AV/MDR/backup coverage — this test would catch it.
func TestHostIntegrationStatusAuthz(t *testing.T) {
	ds := new(mock.Store)
	svc, ctx := newTestService(t, ds, nil, nil)

	teamHost := &fleet.Host{ID: 1, TeamID: new(uint(1))}
	globalHost := &fleet.Host{ID: 2}
	ds.AppConfigFunc = func(ctx context.Context) (*fleet.AppConfig, error) { return &fleet.AppConfig{}, nil }
	ds.HostLiteFunc = func(ctx context.Context, id uint) (*fleet.Host, error) {
		if id == 1 {
			return teamHost, nil
		}
		return globalHost, nil
	}
	ds.ListHostIntegrationStatusFunc = func(ctx context.Context, hostID uint) ([]*fleet.HostIntegrationStatus, error) {
		return nil, nil
	}

	cases := []struct {
		name           string
		user           *fleet.User
		failTeamHost   bool // reading host id 1 (team 1)
		failGlobalHost bool // reading host id 2 (no team)
	}{
		{"global admin", &fleet.User{GlobalRole: new(fleet.RoleAdmin)}, false, false},
		{"global observer", &fleet.User{GlobalRole: new(fleet.RoleObserver)}, false, false},
		{"team 1 observer", &fleet.User{Teams: []fleet.UserTeam{{Team: fleet.Team{ID: 1}, Role: fleet.RoleObserver}}}, false, true},
		{"team 2 observer", &fleet.User{Teams: []fleet.UserTeam{{Team: fleet.Team{ID: 2}, Role: fleet.RoleObserver}}}, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			uctx := viewer.NewContext(ctx, viewer.Viewer{User: c.user})
			_, err := svc.HostIntegrationStatus(uctx, 1)
			checkAuthErr(t, c.failTeamHost, err)
			_, err = svc.HostIntegrationStatus(uctx, 2)
			checkAuthErr(t, c.failGlobalHost, err)
		})
	}
}

// TestAggregatedHostIntegrationStatusService pins the summary-endpoint branches that were the July-11
// security fix: it always passes the VIEWER's team filter to the datastore (never fleet-wide for a
// team-scoped user), 404s a nonexistent team via TeamLite, skips the TeamLite lookup for team_id 0
// ("no team"), and errors without a viewer.
func TestAggregatedHostIntegrationStatusService(t *testing.T) {
	ds := new(mock.Store)
	svc, ctx := newTestService(t, ds, nil, nil)
	ds.AppConfigFunc = func(ctx context.Context) (*fleet.AppConfig, error) { return &fleet.AppConfig{}, nil }

	var gotFilter fleet.TeamFilter
	var gotTeamID *uint
	ds.AggregatedHostIntegrationStatusFunc = func(ctx context.Context, filter fleet.TeamFilter, teamID *uint) ([]*fleet.AggregatedIntegrationStatus, error) {
		gotFilter, gotTeamID = filter, teamID
		return nil, nil
	}

	admin := &fleet.User{GlobalRole: new(fleet.RoleAdmin)}

	t.Run("passes the viewer's team filter to the datastore", func(t *testing.T) {
		ds.TeamLiteFuncInvoked = false
		uctx := viewer.NewContext(ctx, viewer.Viewer{User: admin})
		_, err := svc.AggregatedHostIntegrationStatus(uctx, nil)
		require.NoError(t, err)
		require.Equal(t, admin, gotFilter.User)
		require.True(t, gotFilter.IncludeObserver)
		require.Nil(t, gotTeamID)
		require.False(t, ds.TeamLiteFuncInvoked, "no team_id → no team lookup")
	})

	t.Run("team_id 0 (no team) skips the TeamLite lookup", func(t *testing.T) {
		ds.TeamLiteFuncInvoked = false
		uctx := viewer.NewContext(ctx, viewer.Viewer{User: admin})
		_, err := svc.AggregatedHostIntegrationStatus(uctx, new(uint(0)))
		require.NoError(t, err)
		require.False(t, ds.TeamLiteFuncInvoked, "team_id=0 means hosts with no team; there is no team to look up")
		require.NotNil(t, gotTeamID)
		require.Equal(t, uint(0), *gotTeamID)
	})

	t.Run("nonexistent team → not-found (404) and no rollup query", func(t *testing.T) {
		ds.AggregatedHostIntegrationStatusFuncInvoked = false
		ds.TeamLiteFunc = func(ctx context.Context, id uint) (*fleet.TeamLite, error) {
			return nil, newNotFoundError()
		}
		uctx := viewer.NewContext(ctx, viewer.Viewer{User: admin})
		_, err := svc.AggregatedHostIntegrationStatus(uctx, new(uint(999)))
		require.Error(t, err)
		require.True(t, fleet.IsNotFound(err))
		require.False(t, ds.AggregatedHostIntegrationStatusFuncInvoked, "must not run the rollup for a bogus team")
	})

	t.Run("existing team is looked up then rolled up", func(t *testing.T) {
		ds.TeamLiteFunc = func(ctx context.Context, id uint) (*fleet.TeamLite, error) {
			return &fleet.TeamLite{ID: id}, nil
		}
		uctx := viewer.NewContext(ctx, viewer.Viewer{User: admin})
		_, err := svc.AggregatedHostIntegrationStatus(uctx, new(uint(5)))
		require.NoError(t, err)
		require.True(t, ds.TeamLiteFuncInvoked)
		require.NotNil(t, gotTeamID)
		require.Equal(t, uint(5), *gotTeamID)
	})

	t.Run("no viewer in context → error, no rollup", func(t *testing.T) {
		// With no viewer the authz gate denies first (forbidden); either way the security property is
		// that an unauthenticated call never runs the rollup.
		ds.AggregatedHostIntegrationStatusFuncInvoked = false
		_, err := svc.AggregatedHostIntegrationStatus(ctx, nil)
		require.Error(t, err)
		require.False(t, ds.AggregatedHostIntegrationStatusFuncInvoked)
	})
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

func TestApplyIntegrationStalenessBoundary(t *testing.T) {
	// The freshness boundary is inclusive-stale (age >= ttl ⇒ stale), matching the SQL coverageFreshExpr
	// (fresh iff updated_at strictly newer than NOW-ttl) so the host-detail read path and the coverage
	// filters/rollup can never disagree at the boundary.
	now := time.Now()
	ttl := fleet.IntegrationStaleTTL[fleet.IntegrationCategoryAV]

	atBoundary := &fleet.HostIntegrationStatus{Category: fleet.IntegrationCategoryAV, State: fleet.IntegrationStateProtected, UpdatedAt: now.Add(-ttl)}
	justFresh := &fleet.HostIntegrationStatus{Category: fleet.IntegrationCategoryAV, State: fleet.IntegrationStateProtected, UpdatedAt: now.Add(-ttl + time.Nanosecond)}

	applyIntegrationStaleness([]*fleet.HostIntegrationStatus{atBoundary, justFresh}, now)

	require.True(t, atBoundary.Stale, "age exactly == ttl must be stale (inclusive boundary)")
	require.Equal(t, fleet.IntegrationStateUnknown, atBoundary.State)
	require.False(t, justFresh.Stale, "age just under ttl must stay fresh")
	require.Equal(t, fleet.IntegrationStateProtected, justFresh.State)
}
