package service

import (
	"context"
	"fmt"
	"net/http"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/fleetdm/fleet/v4/server/test"
	"github.com/stretchr/testify/require"
)

// aggKey renders a summary row as "source/category/state" for map-based assertions.
func aggKey(a *fleet.AggregatedIntegrationStatus) string {
	return a.Source + "/" + string(a.Category) + "/" + string(a.State)
}

func aggMap(rows []*fleet.AggregatedIntegrationStatus) map[string]uint {
	m := make(map[string]uint, len(rows))
	for _, r := range rows {
		m[aggKey(r)] = r.Count
	}
	return m
}

// TestHostCoverageEndpoints exercises the community coverage endpoints end-to-end over HTTP: the
// summary rollup's team scoping (including the July-11 fixes — viewer isolation, team_id=0 → no-team,
// 404 for a bogus team), the standard host-list coverage_problems filter, and query-param validation.
func (s *integrationTestSuite) TestHostCoverageEndpoints() {
	t := s.T()
	ctx := context.Background()

	hosts := s.createHosts(t) // 3 hosts: [0] debian, [1] rhel, [2] linux
	team1, err := s.ds.NewTeam(ctx, &fleet.Team{Name: t.Name() + "team1"})
	require.NoError(t, err)
	team2, err := s.ds.NewTeam(ctx, &fleet.Team{Name: t.Name() + "team2"})
	require.NoError(t, err)
	require.NoError(t, s.ds.AddHostsToTeam(ctx, fleet.NewAddHostsToTeamParams(&team1.ID, []uint{hosts[0].ID})))
	require.NoError(t, s.ds.AddHostsToTeam(ctx, fleet.NewAddHostsToTeamParams(&team2.ID, []uint{hosts[1].ID})))
	// hosts[2] stays on "no team".

	// Seed one cell per host: team1 host has a problem (at_risk), team2 host is protected, no-team host
	// has a problem in a different category.
	seed := func(hostID uint, cat fleet.IntegrationCategory, state fleet.IntegrationState) {
		require.NoError(t, s.ds.SetOrUpdateHostIntegrationStatus(ctx, &fleet.HostIntegrationStatus{
			HostID: hostID, Source: "testsrc", Category: cat, State: state, Detail: "d",
		}))
	}
	seed(hosts[0].ID, fleet.IntegrationCategoryAV, fleet.IntegrationStateAtRisk)    // team1
	seed(hosts[1].ID, fleet.IntegrationCategoryAV, fleet.IntegrationStateProtected) // team2
	seed(hosts[2].ID, fleet.IntegrationCategoryMDR, fleet.IntegrationStateAtRisk)   // no team

	summary := func(status int, params ...string) map[string]uint {
		var resp getIntegrationStatusSummaryResponse
		s.DoJSON("GET", "/api/latest/fleet/host_integration_status/summary", nil, status, &resp, params...)
		return aggMap(resp.Summary)
	}

	// --- As global admin: team scoping of the rollup ---
	all := summary(http.StatusOK)
	require.Equal(t, uint(1), all["testsrc/av/at_risk"])
	require.Equal(t, uint(1), all["testsrc/av/protected"])
	require.Equal(t, uint(1), all["testsrc/mdr/at_risk"])

	require.Equal(t, map[string]uint{"testsrc/av/at_risk": 1}, summary(http.StatusOK, "team_id", fmt.Sprint(team1.ID)))
	require.Equal(t, map[string]uint{"testsrc/av/protected": 1}, summary(http.StatusOK, "team_id", fmt.Sprint(team2.ID)))
	// team_id=0 → hosts with no team.
	require.Equal(t, map[string]uint{"testsrc/mdr/at_risk": 1}, summary(http.StatusOK, "team_id", "0"))
	// Nonexistent team → 404 (mirrors /macadmins).
	summary(http.StatusNotFound, "team_id", "999999")

	// --- Standard host-list coverage_problems filter (the shipped, team-scoped path) ---
	var listResp listHostsResponse
	s.DoJSON("GET", "/api/latest/fleet/hosts", nil, http.StatusOK, &listResp, "coverage_problems", "true")
	got := map[uint]bool{}
	for _, h := range listResp.Hosts {
		got[h.ID] = true
	}
	require.True(t, got[hosts[0].ID], "team1 host with an at_risk cell is a problem device")
	require.True(t, got[hosts[2].ID], "no-team host with an at_risk cell is a problem device")
	require.False(t, got[hosts[1].ID], "team2 host with a fresh protected cell is NOT a problem device")

	// Validation: an unknown category is a 400, not a match-everything filter.
	s.DoJSON("GET", "/api/latest/fleet/hosts", nil, http.StatusBadRequest, &listResp, "coverage_missing", "avv")

	// --- Team isolation: a team1-only observer must never see team2's coverage ---
	email := "host-coverage-observer@example.com"
	observer := &fleet.User{
		Name:       "coverage observer",
		Email:      email,
		GlobalRole: nil,
		Teams:      []fleet.UserTeam{{Team: *team1, Role: fleet.RoleObserver}},
	}
	require.NoError(t, observer.SetPassword(test.GoodPassword, 10, 10))
	_, err = s.ds.NewUser(ctx, observer)
	require.NoError(t, err)

	oldToken := s.token
	s.token = s.getTestToken(email, test.GoodPassword)
	defer func() { s.token = oldToken }()

	// No team_id: the viewer filter scopes to team1 only — team2's protected cell and the no-team cell
	// must NOT appear (this is the cross-tenant-leak regression guard).
	obsAll := summary(http.StatusOK)
	require.Equal(t, map[string]uint{"testsrc/av/at_risk": 1}, obsAll)

	// Explicitly requesting team2: the coarse gate passes but the viewer filter yields an empty rollup —
	// the observer sees nothing for a team they are not on, rather than team2's data.
	require.Empty(t, summary(http.StatusOK, "team_id", fmt.Sprint(team2.ID)))
}

// TestHostListPopulateIntegrationStatus verifies the per-host coverage GRID data path: the standard hosts
// list attaches each host's coverage cells only when populate_integration_status=true, and validates the
// param. (Staleness of the attached cells reuses the same svc.HostIntegrationStatus path covered by
// TestApplyIntegrationStaleness and the datastore DB test.)
func (s *integrationTestSuite) TestHostListPopulateIntegrationStatus() {
	t := s.T()
	ctx := context.Background()

	hosts := s.createHosts(t)
	require.NoError(t, s.ds.SetOrUpdateHostIntegrationStatus(ctx, &fleet.HostIntegrationStatus{
		HostID: hosts[0].ID, Source: "screenconnect", Category: fleet.IntegrationCategoryRemoteAccess,
		State: fleet.IntegrationStateProtected, Detail: "agent online",
	}))

	cellsByHost := func(params ...string) map[uint][]*fleet.HostIntegrationStatus {
		var resp listHostsResponse
		s.DoJSON("GET", "/api/latest/fleet/hosts", nil, http.StatusOK, &resp, params...)
		m := map[uint][]*fleet.HostIntegrationStatus{}
		for _, h := range resp.Hosts {
			m[h.ID] = h.IntegrationStatus
		}
		return m
	}

	// Default: coverage cells are NOT attached (omitempty) — the grid opts in explicitly.
	for id, cells := range cellsByHost() {
		require.Nil(t, cells, "host %d must have no integration_status without the param", id)
	}

	// populate_integration_status=true: host[0] carries its cell; a host with no cells gets none.
	withStatus := cellsByHost("populate_integration_status", "true")
	require.Len(t, withStatus[hosts[0].ID], 1)
	cell := withStatus[hosts[0].ID][0]
	require.Equal(t, "screenconnect", cell.Source)
	require.Equal(t, fleet.IntegrationCategoryRemoteAccess, cell.Category)
	require.Equal(t, fleet.IntegrationStateProtected, cell.State)
	require.Empty(t, withStatus[hosts[1].ID])

	// Invalid boolean is a 400, not a silent no-op.
	var resp listHostsResponse
	s.DoJSON("GET", "/api/latest/fleet/hosts", nil, http.StatusBadRequest, &resp, "populate_integration_status", "maybe")
}
