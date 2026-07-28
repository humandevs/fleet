package community

import (
	"context"
	"log/slog"
	"strings"

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
		matched, unmatched := r.persist(ctx, p.Source(), reports)
		// A systemic mismatch (reports came back but NONE matched a Fleet host) is almost always a
		// hostname/FQDN convention problem, not "vendor not deployed" — surface it at WARN so it is
		// visible without DEBUG logging, instead of the whole coverage column silently showing RED.
		if matched == 0 && unmatched > 0 {
			r.logger.WarnContext(ctx, "community host-status: no vendor hosts matched a Fleet host (check hostname/FQDN convention)",
				"source", p.Source(), "unmatched", unmatched)
		}
	}
	return nil
}

// persist resolves and writes one provider's reports, returning how many reports matched a Fleet host
// and how many did not. A single upsert failure is logged and does NOT stop the remaining reports.
func (r *Runner) persist(ctx context.Context, source string, reports []HostStatusReport) (matched, unmatched int) {
	for _, rep := range reports {
		host, err := r.resolve(ctx, rep)
		if err != nil {
			// Unmatched host is expected in normal operation (a vendor may cover devices Fleet doesn't
			// enroll) — debug per-report; the per-run summary in Run flags a total mismatch at WARN.
			unmatched++
			r.logger.DebugContext(ctx, "community host-status report did not match a Fleet host",
				"source", source, "identifier", rep.Identifier, "kind", rep.IdentifierKind, "err", err)
			continue
		}
		matched++
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
	return matched, unmatched
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
	if err == nil {
		return host, nil
	}
	// Vendors often report an FQDN ("DESKTOP-01.corp.local") while Fleet stores the short hostname
	// ("DESKTOP-01"). On a not-found for a dotted identifier, retry with the first DNS label so a whole
	// fleet's coverage column doesn't silently show never-reported RED. Exact match is tried first, so
	// this never overrides a real exact hit.
	if fleet.IsNotFound(err) {
		if short, _, found := strings.Cut(rep.Identifier, "."); found && short != "" {
			if h2, err2 := r.ds.HostByIdentifier(ctx, short); err2 == nil {
				return h2, nil
			}
		}
	}
	return host, ctxerr.Wrap(ctx, err, "resolve host by identifier")
}
