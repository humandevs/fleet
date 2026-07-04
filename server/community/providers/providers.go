// Package providers wires the first-party community host-status providers into a registry. This is
// fork-only composition: per human/PLUGINS.md §4 the registry/loader stays fork-side; only the
// HostStatusProvider interface + the host_integration_status table are the upstream candidate.
package providers

import (
	"github.com/fleetdm/fleet/v4/server/community"
	"github.com/fleetdm/fleet/v4/server/community/action1"
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
	Bitdefender   bitdefender.Config
	Action1       action1.Config
}

// Register adds the first-party community host-status providers to r. ScreenConnect (remote access),
// Bitdefender GravityZone (av/mdr), and Action1 (patching) are functional Collectors — they no-op when
// unconfigured. Huntress/Veeam/iDrive360/WARP are metadata-only scaffolds (declare their column, no
// ingestion yet). Real-vs-mock status is documented per human/setup/*. It validates provider config up
// front — a missing self-hosted ScreenConnect URL fails here rather than producing broken installs.
func Register(r *community.Registry, cfg Config) error {
	if err := cfg.ScreenConnect.Validate(); err != nil {
		return err
	}
	r.RegisterHostStatusProvider(screenconnect.New(cfg.ScreenConnect)) // remote_access (functional)
	r.RegisterHostStatusProvider(bitdefender.New(cfg.Bitdefender))     // av + mdr      (functional)
	r.RegisterHostStatusProvider(action1.New(cfg.Action1))             // patching      (functional)
	r.RegisterHostStatusProvider(huntress.New())                       // mdr           (scaffold)
	r.RegisterHostStatusProvider(veeam.New())                          // backups       (scaffold)
	r.RegisterHostStatusProvider(idrive360.New())                      // backups       (scaffold)
	r.RegisterHostStatusProvider(warp.New())                           // remote_access (scaffold)
	return nil
}
