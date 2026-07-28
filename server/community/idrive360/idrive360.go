// Package idrive360 is a mock community provider scaffold for iDrive360 endpoint backups. The
// HostStatusProvider seam is implemented; the MSP REST API poller is a follow-up. See
// human/setup/backups.md.
package idrive360

import (
	"context"

	"github.com/fleetdm/fleet/v4/server/fleet"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Source() string { return "idrive360" }

func (p *Provider) Categories() []fleet.IntegrationCategory {
	return []fleet.IntegrationCategory{fleet.IntegrationCategoryBackups}
}

// Sync is not yet implemented (mock).
func (p *Provider) Sync(ctx context.Context, ds fleet.Datastore) error { _ = ctx; _ = ds; return nil }
