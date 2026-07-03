// Package bitdefender is a mock community provider scaffold for Bitdefender GravityZone (AV/EDR). The
// HostStatusProvider seam is implemented; the GravityZone JSON-RPC ingestion is a follow-up. See
// human/setup/bitdefender-gravityzone.md.
package bitdefender

import (
	"context"

	"github.com/fleetdm/fleet/v4/server/fleet"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Source() string { return "bitdefender" }

func (p *Provider) Categories() []fleet.IntegrationCategory {
	return []fleet.IntegrationCategory{fleet.IntegrationCategoryAV}
}

// Sync is not yet implemented (mock).
func (p *Provider) Sync(ctx context.Context, ds fleet.Datastore) error { _ = ctx; _ = ds; return nil }
