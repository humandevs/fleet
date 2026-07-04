// Package community defines the community-plugin seams for the Fleet fork. Community plugins are
// MIT-licensed, do NOT require the enterprise (premium) license, and coexist with it. A community
// plugin may ship free or as a paid product; either way it plugs into these interfaces and never
// depends on ee/. See human/PLUGIN-API-RFC.md and human/MVP.md.
package community

import (
	"context"

	"github.com/fleetdm/fleet/v4/server/fleet"
)

// HostStatusProvider is a community plugin that reports host coverage status (AV / MDR / remote
// access / backups / disk encryption) into the host_integration_status store, feeding the coverage
// matrix. Registration only declares WHICH coverage columns a provider owns (Source + Categories); a
// provider that can actually pull data additionally implements Collector. This two-part split lets a
// plugin register its columns before its ingestion is built (our mock scaffolds do exactly that), and
// keeps the metadata contract stable if a provider is later moved out-of-process
// (human/PLUGIN-API-RFC.md). Providers never touch the DB or the host read path themselves — the Runner
// persists what Collect returns.
type HostStatusProvider interface {
	// Source returns the stable provider key (e.g. "bitdefender"), matching the value written to
	// HostIntegrationStatus.Source.
	Source() string
	// Categories returns the coverage categories this provider owns.
	Categories() []fleet.IntegrationCategory
}

// Collector is the optional data-bearing half of a provider: a provider that can pull current coverage
// from its vendor API implements it. The Runner calls Collect on its own cadence, resolves each report's
// host identifier to a Fleet host, and upserts the cell. Keeping Collect free of DB access (it returns
// data instead of writing) is what makes a provider unit-testable against an httptest vendor server and
// portable to an out-of-process transport later.
type Collector interface {
	// Collect pulls current coverage from the provider's vendor API and returns one report per host it
	// knows about. Returning an empty slice is valid (nothing to report). It must not access the Fleet DB.
	Collect(ctx context.Context) ([]HostStatusReport, error)
}

// IdentifierKind tells the Runner which host field a report's Identifier should resolve against.
type IdentifierKind string

const (
	// IdentifierAny resolves via Datastore.HostByIdentifier (fallback chain: osquery id, node key, UUID,
	// hardware serial, hostname). The right default when a vendor's key could be any of these.
	IdentifierAny IdentifierKind = "any"
	// IdentifierHostname resolves against hostname (via HostByIdentifier). ScreenConnect uses this: we set
	// t=<Fleet hostname> at install, so the session name IS the hostname.
	IdentifierHostname IdentifierKind = "hostname"
	// IdentifierUUID resolves against the uuid column exactly (via HostByUUID).
	IdentifierUUID IdentifierKind = "uuid"
	// IdentifierSerial resolves against hardware_serial (via HostByIdentifier's fallback chain).
	IdentifierSerial IdentifierKind = "serial"
)

// HostStatusReport is one coverage reading a provider pulled from its vendor API, keyed by a host
// identifier the vendor knows (hostname / UUID / serial). The Runner resolves it to a Fleet host_id and
// upserts a HostIntegrationStatus cell for (host, Source, Category).
type HostStatusReport struct {
	// Identifier is the vendor's handle for the host (e.g. the ScreenConnect session name / machine name,
	// the GravityZone endpoint FQDN or serial).
	Identifier string
	// IdentifierKind selects how the Runner resolves Identifier to a Fleet host.
	IdentifierKind IdentifierKind
	// Category is the coverage column this reading fills.
	Category fleet.IntegrationCategory
	// State is the normalized coverage value.
	State fleet.IntegrationState
	// Detail is optional human context (e.g. "last seen 3m ago", agent version) shown on hover.
	Detail string
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
