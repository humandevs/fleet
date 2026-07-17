package mysql

import (
	"context"
	"testing"
	"time"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/fleetdm/fleet/v4/server/test"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

// TestHostIntegrationStatusDB executes the coverage SQL against a real MySQL (MYSQL_TEST=1):
// upserts, the staleness-aware aggregate rollup, viewer/team scoping (including team 0 = no team),
// and the "problem devices" filter including never-reported hosts.
func TestHostIntegrationStatusDB(t *testing.T) {
	ds := CreateMySQLDS(t)
	ctx := t.Context()

	team, err := ds.NewTeam(ctx, &fleet.Team{Name: "coverage-team"})
	require.NoError(t, err)

	now := time.Now()
	h1 := test.NewHost(t, ds, "h1.coverage.local", "1.1.1.1", "cov-key-1", "cov-uuid-1", now) // team, fresh protected
	h2 := test.NewHost(t, ds, "h2.coverage.local", "1.1.1.2", "cov-key-2", "cov-uuid-2", now) // no team, at_risk
	h3 := test.NewHost(t, ds, "h3.coverage.local", "1.1.1.3", "cov-key-3", "cov-uuid-3", now) // no team, stale protected
	h4 := test.NewHost(t, ds, "h4.coverage.local", "1.1.1.4", "cov-key-4", "cov-uuid-4", now) // no team, never reported
	require.NoError(t, ds.AddHostsToTeam(ctx, fleet.NewAddHostsToTeamParams(&team.ID, []uint{h1.ID})))

	upsert := func(hostID uint, cat fleet.IntegrationCategory, state fleet.IntegrationState) {
		require.NoError(t, ds.SetOrUpdateHostIntegrationStatus(ctx, &fleet.HostIntegrationStatus{
			HostID: hostID, Source: "testsrc", Category: cat, State: state, Detail: "d",
		}))
	}
	upsert(h1.ID, fleet.IntegrationCategoryAV, fleet.IntegrationStateProtected)
	upsert(h2.ID, fleet.IntegrationCategoryRemoteAccess, fleet.IntegrationStateAtRisk)
	upsert(h3.ID, fleet.IntegrationCategoryAV, fleet.IntegrationStateProtected)

	// Age h3's cell past the av TTL (2h) so it must be counted/filtered as "unknown".
	ExecAdhocSQL(t, ds, func(q sqlx.ExtContext) error {
		_, err := q.ExecContext(context.Background(),
			"UPDATE host_integration_status SET updated_at = NOW(6) - INTERVAL 3 HOUR WHERE host_id = ?", h3.ID)
		return err
	})

	filter := fleet.TeamFilter{User: test.UserAdmin}

	t.Run("list per host", func(t *testing.T) {
		rows, err := ds.ListHostIntegrationStatus(ctx, h1.ID)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, fleet.IntegrationCategoryAV, rows[0].Category)
		require.Equal(t, fleet.IntegrationStateProtected, rows[0].State)
	})

	aggKey := func(a *fleet.AggregatedIntegrationStatus) string {
		return a.Source + "/" + string(a.Category) + "/" + string(a.State)
	}
	toMap := func(agg []*fleet.AggregatedIntegrationStatus) map[string]uint {
		m := make(map[string]uint, len(agg))
		for _, a := range agg {
			m[aggKey(a)] = a.Count
		}
		return m
	}

	t.Run("aggregate applies staleness and team scoping", func(t *testing.T) {
		// Global rollup: the stale protected cell (h3) must be counted as unknown, not protected.
		agg, err := ds.AggregatedHostIntegrationStatus(ctx, filter, nil)
		require.NoError(t, err)
		require.Equal(t, map[string]uint{
			"testsrc/av/protected":          1, // h1 (fresh)
			"testsrc/av/unknown":            1, // h3 (stale past TTL)
			"testsrc/remote_access/at_risk": 1, // h2
		}, toMap(agg))

		// Team-scoped: only h1's cell.
		agg, err = ds.AggregatedHostIntegrationStatus(ctx, filter, &team.ID)
		require.NoError(t, err)
		require.Equal(t, map[string]uint{"testsrc/av/protected": 1}, toMap(agg))

		// Team 0 = hosts with no team: h2 + h3.
		agg, err = ds.AggregatedHostIntegrationStatus(ctx, filter, new(uint(0)))
		require.NoError(t, err)
		require.Equal(t, map[string]uint{
			"testsrc/av/unknown":            1,
			"testsrc/remote_access/at_risk": 1,
		}, toMap(agg))

		// A viewer with no team roles sees nothing, regardless of the team param.
		noRoleUser := &fleet.User{ID: 999999}
		agg, err = ds.AggregatedHostIntegrationStatus(ctx, fleet.TeamFilter{User: noRoleUser}, nil)
		require.NoError(t, err)
		require.Empty(t, agg)
	})

	t.Run("problem devices includes bad, stale, and never-reported hosts", func(t *testing.T) {
		// Direct ID filter.
		ids, err := ds.ListHostsByCoverage(ctx, fleet.CoverageFilter{Problems: true})
		require.NoError(t, err)
		require.ElementsMatch(t, []uint{h2.ID, h3.ID, h4.ID}, ids)

		// Through the standard host list (what the UI uses), same result set.
		opt := fleet.HostListOptions{CoverageFilter: fleet.CoverageFilter{Problems: true}}
		hosts, err := ds.ListHosts(ctx, filter, opt)
		require.NoError(t, err)
		gotIDs := make([]uint, 0, len(hosts))
		for _, h := range hosts {
			gotIDs = append(gotIDs, h.ID)
		}
		require.ElementsMatch(t, []uint{h2.ID, h3.ID, h4.ID}, gotIDs)

		count, err := ds.CountHosts(ctx, filter, opt)
		require.NoError(t, err)
		require.Equal(t, 3, count)
	})

	t.Run("missing category filter", func(t *testing.T) {
		// Hosts lacking a fresh protected av cell: everyone but h1 (h3's av is stale).
		ids, err := ds.ListHostsByCoverage(ctx, fleet.CoverageFilter{
			MissingCategories: []fleet.IntegrationCategory{fleet.IntegrationCategoryAV},
		})
		require.NoError(t, err)
		require.ElementsMatch(t, []uint{h2.ID, h3.ID, h4.ID}, ids)
	})
}
