package screenconnect

import (
	"os"
	"testing"

	"github.com/fleetdm/fleet/v4/server/fleet"
)

// TestLiveQuery is a MANUAL, env-gated probe against the real ScreenConnect instance — it skips unless
// SC_LIVE_URL is set. Used to verify the TestFleet group end-to-end. DELETE before committing.
func TestLiveQuery(t *testing.T) {
	url := os.Getenv("SC_LIVE_URL")
	if url == "" {
		t.Skip("set SC_LIVE_URL / SC_LIVE_SECRET / SC_LIVE_FILTER to run")
	}
	p := New(Config{
		InstanceURL:   url,
		AccessSecret:  os.Getenv("SC_LIVE_SECRET"),
		SessionFilter: os.Getenv("SC_LIVE_FILTER"),
	})
	reports, err := p.Collect(t.Context())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	online, offline := 0, 0
	for _, r := range reports {
		if r.State == fleet.IntegrationStateProtected {
			online++
		} else {
			offline++
		}
		t.Logf("  %-28s %s  (%s)", r.Identifier, r.State, r.Category)
	}
	t.Logf("LIVE: %d reports for filter %q (%d online / %d offline)", len(reports), os.Getenv("SC_LIVE_FILTER"), online, offline)
}
