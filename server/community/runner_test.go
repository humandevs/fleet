package community

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/fleetdm/fleet/v4/server/mock"
	"github.com/stretchr/testify/require"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// fakeProvider is a Collector-implementing provider for runner tests.
type fakeProvider struct {
	source  string
	reports []HostStatusReport
	err     error
}

func (f *fakeProvider) Source() string                          { return f.source }
func (f *fakeProvider) Categories() []fleet.IntegrationCategory { return nil }
func (f *fakeProvider) Collect(context.Context) ([]HostStatusReport, error) {
	return f.reports, f.err
}

// metaOnlyProvider declares columns but has no ingestion (does NOT implement Collector).
type metaOnlyProvider struct{}

func (metaOnlyProvider) Source() string                          { return "meta" }
func (metaOnlyProvider) Categories() []fleet.IntegrationCategory { return nil }

func TestRunnerResolvesAndPersists(t *testing.T) {
	ds := new(mock.DataStore)
	var upserts []*fleet.HostIntegrationStatus
	ds.HostByIdentifierFunc = func(_ context.Context, id string) (*fleet.Host, error) {
		if id == "DESKTOP-01" {
			return &fleet.Host{ID: 7}, nil
		}
		return nil, errors.New("not found")
	}
	ds.SetOrUpdateHostIntegrationStatusFunc = func(_ context.Context, s *fleet.HostIntegrationStatus) error {
		upserts = append(upserts, s)
		return nil
	}

	reg := NewRegistry()
	reg.RegisterHostStatusProvider(&fakeProvider{
		source: "screenconnect",
		reports: []HostStatusReport{
			{Identifier: "DESKTOP-01", IdentifierKind: IdentifierHostname, Category: fleet.IntegrationCategoryRemoteAccess, State: fleet.IntegrationStateProtected},
			{Identifier: "GHOST-99", IdentifierKind: IdentifierHostname, Category: fleet.IntegrationCategoryRemoteAccess, State: fleet.IntegrationStateAtRisk},
		},
	})
	reg.RegisterHostStatusProvider(metaOnlyProvider{}) // no Collector → skipped, not an error

	require.NoError(t, NewRunner(reg, ds, discardLogger()).Run(t.Context()))

	// Only the host that resolved gets a cell; the unmatched vendor device is skipped.
	require.Len(t, upserts, 1)
	require.Equal(t, uint(7), upserts[0].HostID)
	require.Equal(t, "screenconnect", upserts[0].Source)
	require.Equal(t, fleet.IntegrationCategoryRemoteAccess, upserts[0].Category)
	require.Equal(t, fleet.IntegrationStateProtected, upserts[0].State)
}

func TestRunnerUsesHostByUUIDForUUIDKind(t *testing.T) {
	ds := new(mock.DataStore)
	ds.HostByUUIDFunc = func(_ context.Context, uuid string) (*fleet.Host, error) {
		require.Equal(t, "ABC-UUID", uuid)
		return &fleet.Host{ID: 3}, nil
	}
	ds.SetOrUpdateHostIntegrationStatusFunc = func(_ context.Context, _ *fleet.HostIntegrationStatus) error { return nil }

	reg := NewRegistry()
	reg.RegisterHostStatusProvider(&fakeProvider{
		source:  "bitdefender",
		reports: []HostStatusReport{{Identifier: "ABC-UUID", IdentifierKind: IdentifierUUID, Category: fleet.IntegrationCategoryAV, State: fleet.IntegrationStateProtected}},
	})
	require.NoError(t, NewRunner(reg, ds, discardLogger()).Run(t.Context()))
	require.True(t, ds.HostByUUIDFuncInvoked)
	require.False(t, ds.HostByIdentifierFuncInvoked)
}

func TestRunnerSkipsProviderOnCollectError(t *testing.T) {
	ds := new(mock.DataStore)
	ds.SetOrUpdateHostIntegrationStatusFunc = func(_ context.Context, _ *fleet.HostIntegrationStatus) error { return nil }

	reg := NewRegistry()
	reg.RegisterHostStatusProvider(&fakeProvider{source: "boom", err: errors.New("api down")})

	// A provider whose API is down is logged and skipped — Run still succeeds and writes nothing.
	require.NoError(t, NewRunner(reg, ds, discardLogger()).Run(t.Context()))
	require.False(t, ds.SetOrUpdateHostIntegrationStatusFuncInvoked)
}

// notFoundErr implements fleet.IsNotFound so the runner's FQDN fallback recognizes a miss.
type notFoundErr struct{}

func (notFoundErr) Error() string    { return "not found" }
func (notFoundErr) IsNotFound() bool { return true }

// TestRunnerContinuesAfterUpsertError pins the resilience guarantee that one failed upsert does NOT
// abort the rest of a provider's batch — otherwise a single transient DB error would drop every
// subsequent host's cell that run, and those cells would drift to "unknown" past their TTL.
func TestRunnerContinuesAfterUpsertError(t *testing.T) {
	ds := new(mock.DataStore)
	ds.HostByIdentifierFunc = func(_ context.Context, id string) (*fleet.Host, error) {
		return &fleet.Host{ID: 1}, nil
	}
	var calls int
	ds.SetOrUpdateHostIntegrationStatusFunc = func(_ context.Context, _ *fleet.HostIntegrationStatus) error {
		calls++
		if calls == 1 {
			return errors.New("deadlock")
		}
		return nil
	}

	reg := NewRegistry()
	reg.RegisterHostStatusProvider(&fakeProvider{
		source: "screenconnect",
		reports: []HostStatusReport{
			{Identifier: "A", IdentifierKind: IdentifierHostname, Category: fleet.IntegrationCategoryRemoteAccess, State: fleet.IntegrationStateProtected},
			{Identifier: "B", IdentifierKind: IdentifierHostname, Category: fleet.IntegrationCategoryRemoteAccess, State: fleet.IntegrationStateProtected},
		},
	})

	require.NoError(t, NewRunner(reg, ds, discardLogger()).Run(t.Context()))
	require.Equal(t, 2, calls, "second report must still be upserted after the first upsert errors")
}

// TestRunnerHealthyProviderAfterFailingProvider pins that a provider registered AFTER a failing one
// still runs — a regression that returned the collect error from Run instead of continuing would be
// caught here (the existing skip test registers only the failing provider, so it would not).
func TestRunnerHealthyProviderAfterFailingProvider(t *testing.T) {
	ds := new(mock.DataStore)
	ds.HostByIdentifierFunc = func(_ context.Context, id string) (*fleet.Host, error) { return &fleet.Host{ID: 5}, nil }
	var upserts int
	ds.SetOrUpdateHostIntegrationStatusFunc = func(_ context.Context, _ *fleet.HostIntegrationStatus) error {
		upserts++
		return nil
	}

	reg := NewRegistry()
	reg.RegisterHostStatusProvider(&fakeProvider{source: "boom", err: errors.New("api down")})
	reg.RegisterHostStatusProvider(&fakeProvider{
		source:  "healthy",
		reports: []HostStatusReport{{Identifier: "H", IdentifierKind: IdentifierHostname, Category: fleet.IntegrationCategoryAV, State: fleet.IntegrationStateProtected}},
	})

	require.NoError(t, NewRunner(reg, ds, discardLogger()).Run(t.Context()))
	require.Equal(t, 1, upserts, "the healthy provider after the failing one must still persist its cell")
}

// TestRunnerResolvesFQDNToShortHostname pins the FQDN→short-name fallback: a vendor reporting
// "DESKTOP-01.corp.local" must still match a Fleet host stored as "DESKTOP-01" instead of silently
// showing never-reported RED for the whole coverage column.
func TestRunnerResolvesFQDNToShortHostname(t *testing.T) {
	ds := new(mock.DataStore)
	var queried []string
	ds.HostByIdentifierFunc = func(_ context.Context, id string) (*fleet.Host, error) {
		queried = append(queried, id)
		if id == "DESKTOP-01" {
			return &fleet.Host{ID: 9}, nil
		}
		return nil, notFoundErr{} // the FQDN itself is not found
	}
	var upserts []*fleet.HostIntegrationStatus
	ds.SetOrUpdateHostIntegrationStatusFunc = func(_ context.Context, s *fleet.HostIntegrationStatus) error {
		upserts = append(upserts, s)
		return nil
	}

	reg := NewRegistry()
	reg.RegisterHostStatusProvider(&fakeProvider{
		source:  "bitdefender",
		reports: []HostStatusReport{{Identifier: "DESKTOP-01.corp.local", IdentifierKind: IdentifierHostname, Category: fleet.IntegrationCategoryAV, State: fleet.IntegrationStateProtected}},
	})

	require.NoError(t, NewRunner(reg, ds, discardLogger()).Run(t.Context()))
	require.Equal(t, []string{"DESKTOP-01.corp.local", "DESKTOP-01"}, queried, "must try the FQDN first, then the short name")
	require.Len(t, upserts, 1)
	require.Equal(t, uint(9), upserts[0].HostID)
}
