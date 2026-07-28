// Package warp is a mock community provider scaffold for Cloudflare WARP (Zero Trust device agent).
// It maps to remote_access coverage (installed + enrolled + connected). The HostStatusProvider seam is
// implemented; the osquery/warp-cli status ingestion is a follow-up. See human/CONFIG-MGMT.md and
// human/ZERO-TRUST.md.
package warp

import (
	"context"

	"github.com/fleetdm/fleet/v4/server/fleet"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Source() string { return "warp" }

func (p *Provider) Categories() []fleet.IntegrationCategory {
	return []fleet.IntegrationCategory{fleet.IntegrationCategoryRemoteAccess}
}

// Sync is not yet implemented (mock).
func (p *Provider) Sync(ctx context.Context, ds fleet.Datastore) error { _ = ctx; _ = ds; return nil }
