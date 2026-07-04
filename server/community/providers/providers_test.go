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
		[]string{"screenconnect", "bitdefender", "action1", "huntress", "veeam", "idrive360", "warp"},
		sources,
	)
}

func TestRegisterRequiresSelfHostedURL(t *testing.T) {
	// Self-hosted has no default domain — an unset ScreenConnect URL must stop wiring.
	err := Register(community.NewRegistry(), Config{})
	require.Error(t, err)
}
