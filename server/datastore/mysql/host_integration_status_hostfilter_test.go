package mysql

import (
	"testing"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/stretchr/testify/require"
)

// TestFilterHostsByCoverage covers the pure host-list glue that threads a CoverageFilter into the host
// query (no DB needed — it only assembles SQL fragments + params).
func TestFilterHostsByCoverage(t *testing.T) {
	const base = "SELECT 1 WHERE TRUE"

	// Zero filter is a no-op: SQL and params pass through untouched.
	sql, params := filterHostsByCoverage(base, fleet.HostListOptions{}, []interface{}{"x"})
	require.Equal(t, base, sql)
	require.Equal(t, []interface{}{"x"}, params)

	// "Problem devices" appends a no-cells-or-bad-cell condition against host_integration_status and
	// adds no params.
	sql, params = filterHostsByCoverage(base,
		fleet.HostListOptions{CoverageFilter: fleet.CoverageFilter{Problems: true}}, nil)
	require.Contains(t, sql, base+" AND (NOT EXISTS")
	require.Contains(t, sql, "host_integration_status")
	require.Empty(t, params)

	// A missing-category filter appends a NOT EXISTS condition and the category as a param.
	sql, params = filterHostsByCoverage(base,
		fleet.HostListOptions{CoverageFilter: fleet.CoverageFilter{
			MissingCategories: []fleet.IntegrationCategory{fleet.IntegrationCategoryAV},
		}}, nil)
	require.Contains(t, sql, "NOT EXISTS")
	require.Equal(t, []interface{}{string(fleet.IntegrationCategoryAV)}, params)
}
