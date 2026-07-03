// Package community defines the community-plugin seams for the Fleet fork. Community plugins are
// MIT-licensed, do NOT require the enterprise (premium) license, and coexist with it. A community
// plugin may ship free or as a paid product; either way it plugs into these interfaces and never
// depends on ee/. See human/PLUGIN-API-RFC.md and human/MVP.md.
package community

import "github.com/fleetdm/fleet/v4/server/fleet"

// HostStatusProvider is a community plugin that reports host coverage status (AV / MDR / remote
// access / backups / disk encryption) into the host_integration_status store, feeding the coverage
// matrix. Providers sync on their own cadence and write via
// fleet.Datastore.SetOrUpdateHostIntegrationStatus; they are never called on the host read path.
type HostStatusProvider interface {
	// Source returns the stable provider key (e.g. "bitdefender"), matching the value written to
	// HostIntegrationStatus.Source.
	Source() string
	// Categories returns the coverage categories this provider owns.
	Categories() []fleet.IntegrationCategory
}

// Registry is the compile-time registry of community host-status providers. First-party providers
// register at startup; a sync scheduler iterates it. Kept minimal and transport-agnostic so a
// provider can later be backed out-of-process (see human/PLUGIN-API-RFC.md) without changing call
// sites.
type Registry struct {
	hostStatus []HostStatusProvider
}

// NewRegistry returns an empty community-plugin registry.
func NewRegistry() *Registry { return &Registry{} }

// RegisterHostStatusProvider adds a provider to the registry.
func (r *Registry) RegisterHostStatusProvider(p HostStatusProvider) {
	r.hostStatus = append(r.hostStatus, p)
}

// HostStatusProviders returns the registered providers.
func (r *Registry) HostStatusProviders() []HostStatusProvider { return r.hostStatus }
