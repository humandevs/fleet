// Package veeam is a mock community provider scaffold for Veeam backups (VSPC / Enterprise Manager /
// Veeam Agent). The HostStatusProvider seam is implemented; the API poller (or Event-Log signal) is a
// follow-up. See human/setup/backups.md.
package veeam

import (
	"context"

	"github.com/fleetdm/fleet/v4/server/fleet"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Source() string { return "veeam" }

func (p *Provider) Categories() []fleet.IntegrationCategory {
	return []fleet.IntegrationCategory{fleet.IntegrationCategoryBackups}
}

// Sync is not yet implemented (mock).
func (p *Provider) Sync(ctx context.Context, ds fleet.Datastore) error { _ = ctx; _ = ds; return nil }
