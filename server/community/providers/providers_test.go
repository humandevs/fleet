package providers

import (
	"testing"

	"github.com/fleetdm/fleet/v4/server/community"
	"github.com/fleetdm/fleet/v4/server/community/screenconnect"
	"github.com/stretchr/testify/require"
)

func TestRegister(t *testing.T) {
	r := community.NewRegistry()
	Register(r, Config{ScreenConnect: screenconnect.Config{InstanceURL: "https://example.screenconnect.com"}})

	var sources []string
	for _, p := range r.HostStatusProviders() {
		sources = append(sources, p.Source())
	}
	require.ElementsMatch(t,
		[]string{"screenconnect", "bitdefender", "huntress", "veeam", "idrive360", "warp"},
		sources,
	)
}
