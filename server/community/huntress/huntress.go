// Package huntress is a mock community provider scaffold for Huntress (managed EDR/MDR). The
// HostStatusProvider seam is implemented; the Huntress REST/Svix-webhook ingestion is a follow-up.
// See human/setup/huntress.md.
package huntress

import (
	"context"

	"github.com/fleetdm/fleet/v4/server/fleet"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Source() string { return "huntress" }

func (p *Provider) Categories() []fleet.IntegrationCategory {
	return []fleet.IntegrationCategory{fleet.IntegrationCategoryMDR}
}

// Sync is not yet implemented (mock).
func (p *Provider) Sync(ctx context.Context, ds fleet.Datastore) error { _ = ctx; _ = ds; return nil }
