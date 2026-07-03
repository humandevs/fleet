// Package providers wires the first-party community host-status providers into a registry. This is
// fork-only composition: per human/PLUGINS.md §4 the registry/loader stays fork-side; only the
// HostStatusProvider interface + the host_integration_status table are the upstream candidate.
package providers

import (
	"github.com/fleetdm/fleet/v4/server/community"
	"github.com/fleetdm/fleet/v4/server/community/bitdefender"
	"github.com/fleetdm/fleet/v4/server/community/huntress"
	"github.com/fleetdm/fleet/v4/server/community/idrive360"
	"github.com/fleetdm/fleet/v4/server/community/screenconnect"
	"github.com/fleetdm/fleet/v4/server/community/veeam"
	"github.com/fleetdm/fleet/v4/server/community/warp"
)

// Config holds per-provider configuration for the community registry.
type Config struct {
	ScreenConnect screenconnect.Config
}

// Register adds the first-party community host-status providers to r. ScreenConnect is real
// (deployment); the rest are mocks today, each mapped to one coverage-matrix category. Real-vs-mock
// status is documented per human/setup/*.
func Register(r *community.Registry, cfg Config) {
	r.RegisterHostStatusProvider(screenconnect.New(cfg.ScreenConnect)) // remote_access
	r.RegisterHostStatusProvider(bitdefender.New())                    // av            (mock)
	r.RegisterHostStatusProvider(huntress.New())                       // mdr           (mock)
	r.RegisterHostStatusProvider(veeam.New())                          // backups       (mock)
	r.RegisterHostStatusProvider(idrive360.New())                      // backups       (mock)
	r.RegisterHostStatusProvider(warp.New())                           // remote_access (mock)
}
