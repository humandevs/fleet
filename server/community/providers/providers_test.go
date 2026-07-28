package providers

import (
	"testing"

	"github.com/fleetdm/fleet/v4/server/community"
	"github.com/fleetdm/fleet/v4/server/community/screenconnect"
	"github.com/stretchr/testify/require"
)

func TestRegister(t *testing.T) {
	r := community.NewRegistry()
	require.NoError(t, Register(r, Config{ScreenConnect: screenconnect.Config{InstanceURL: "https://remote.example.com"}}))

	var sources []string
	for _, p := range r.HostStatusProviders() {
		sources = append(sources, p.Source())
	}
	require.ElementsMatch(t,
		[]string{"screenconnect", "splashtop", "bitdefender", "action1", "huntress", "veeam", "idrive360", "warp"},
		sources,
	)
}

func TestRegisterValidatesConfiguredScreenConnectURL(t *testing.T) {
	// An empty config is valid — every provider simply no-ops until configured, so the collector can be
	// wired without forcing ScreenConnect on deployments that only use other providers.
	require.NoError(t, Register(community.NewRegistry(), Config{}))

	// But a CONFIGURED, malformed ScreenConnect URL must still fail loudly (self-hosted has no default
	// domain, so a typo would otherwise produce broken installs).
	err := Register(community.NewRegistry(), Config{ScreenConnect: screenconnect.Config{InstanceURL: "not-a-url"}})
	require.Error(t, err)
}
