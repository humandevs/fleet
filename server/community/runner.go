package community

import (
	"context"
	"log/slog"

	"github.com/fleetdm/fleet/v4/server/contexts/ctxerr"
	"github.com/fleetdm/fleet/v4/server/fleet"
)

// Runner drives the community host-status providers. On each Run it calls Collect on every registered
// provider that implements Collector, resolves each report's host identifier to a Fleet host, and upserts
// the coverage cell via the datastore. It is the single place that touches the DB, so providers stay pure
// (vendor API in, reports out) and independently testable. Schedule Run from a cron (minutes cadence); the
// host read path renders cells older than their category TTL as "unknown", so a missed run degrades to
// "unknown", never to a stale "protected".
type Runner struct {
	reg    *Registry
	ds     fleet.Datastore
	logger *slog.Logger
}

// NewRunner returns a Runner over the given registry and datastore.
func NewRunner(reg *Registry, ds fleet.Datastore, logger *slog.Logger) *Runner {
	return &Runner{reg: reg, ds: ds, logger: logger}
}

// Run collects from every provider once and persists the results. A provider that errors or has an
// unresolvable/unknown host is logged and skipped — one bad provider or unmatched host never aborts the
// rest. It returns nil unless something structural fails; per-item problems are logged, not returned.
func (r *Runner) Run(ctx context.Context) error {
	for _, p := range r.reg.HostStatusProviders() {
		collector, ok := p.(Collector)
		if !ok {
			// Metadata-only scaffold (declares columns, no ingestion yet) — nothing to collect.
			continue
		}
		reports, err := collector.Collect(ctx)
		if err != nil {
			r.logger.WarnContext(ctx, "community host-status collect failed",
				"source", p.Source(), "err", err)
			continue
		}
		r.persist(ctx, p.Source(), reports)
	}
	return nil
}

// persist resolves and writes one provider's reports.
func (r *Runner) persist(ctx context.Context, source string, reports []HostStatusReport) {
	for _, rep := range reports {
		host, err := r.resolve(ctx, rep)
		if err != nil {
			// Unmatched host is expected (a vendor may cover devices Fleet doesn't enroll) — debug, not warn.
			r.logger.DebugContext(ctx, "community host-status report did not match a Fleet host",
				"source", source, "identifier", rep.Identifier, "kind", rep.IdentifierKind, "err", err)
			continue
		}
		cell := &fleet.HostIntegrationStatus{
			HostID:   host.ID,
			Source:   source,
			Category: rep.Category,
			State:    rep.State,
			Detail:   rep.Detail,
		}
		if err := r.ds.SetOrUpdateHostIntegrationStatus(ctx, cell); err != nil {
			r.logger.WarnContext(ctx, "community host-status upsert failed",
				"source", source, "host_id", host.ID, "category", rep.Category, "err", err)
		}
	}
}

// resolve maps a report's vendor identifier to a Fleet host.
func (r *Runner) resolve(ctx context.Context, rep HostStatusReport) (*fleet.Host, error) {
	if rep.IdentifierKind == IdentifierUUID {
		host, err := r.ds.HostByUUID(ctx, rep.Identifier)
		return host, ctxerr.Wrap(ctx, err, "resolve host by uuid")
	}
	// hostname / serial / any all use the HostByIdentifier fallback chain (it covers hostname and
	// hardware_serial), so we don't need vendor-specific lookups for those kinds.
	host, err := r.ds.HostByIdentifier(ctx, rep.Identifier)
	return host, ctxerr.Wrap(ctx, err, "resolve host by identifier")
}
