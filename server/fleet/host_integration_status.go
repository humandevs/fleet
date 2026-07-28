package fleet

import "time"

// IntegrationCategory is a normalized host-coverage dimension surfaced as a column in the host
// coverage matrix. Community-plugin providers report status per category.
type IntegrationCategory string

const (
	IntegrationCategoryAV             IntegrationCategory = "av"
	IntegrationCategoryMDR            IntegrationCategory = "mdr"
	IntegrationCategoryRemoteAccess   IntegrationCategory = "remote_access"
	IntegrationCategoryBackups        IntegrationCategory = "backups"
	IntegrationCategoryDiskEncryption IntegrationCategory = "disk_encryption"
	IntegrationCategoryPatching       IntegrationCategory = "patching"
)

// IntegrationState is the normalized per-cell coverage value. Kept intentionally small so the
// coverage-matrix UI stays vendor-agnostic.
type IntegrationState string

const (
	IntegrationStateProtected    IntegrationState = "protected"
	IntegrationStateAtRisk       IntegrationState = "at_risk"
	IntegrationStateNotInstalled IntegrationState = "not_installed"
	IntegrationStateUnknown      IntegrationState = "unknown"
)

// IntegrationStaleTTL is the single source of truth for per-category freshness windows: a coverage
// cell older than its category's TTL is treated as "unknown" (uncovered) EVERYWHERE — the host-detail
// read path (service applyIntegrationStaleness), the dashboard rollup, and the coverage host filters.
// The datastore's SQL freshness expression is DERIVED from this map (see mysql.buildCoverageFreshExpr),
// so the two surfaces can never drift.
var IntegrationStaleTTL = map[IntegrationCategory]time.Duration{
	IntegrationCategoryAV:             2 * time.Hour,
	IntegrationCategoryMDR:            2 * time.Hour,
	IntegrationCategoryRemoteAccess:   1 * time.Hour,
	IntegrationCategoryBackups:        36 * time.Hour, // backups typically run daily
	IntegrationCategoryDiskEncryption: 24 * time.Hour,
	IntegrationCategoryPatching:       24 * time.Hour,
}

// DefaultIntegrationStaleTTL is the freshness window for any category not in IntegrationStaleTTL.
const DefaultIntegrationStaleTTL = 2 * time.Hour

// IsValid reports whether c is one of the known coverage categories. Used to reject unknown
// category values at the API boundary (a typo like "avv" would otherwise compile to a filter that
// matches every host, and an unbounded list would let a caller force one correlated subquery per
// value).
func (c IntegrationCategory) IsValid() bool {
	switch c {
	case IntegrationCategoryAV, IntegrationCategoryMDR, IntegrationCategoryRemoteAccess,
		IntegrationCategoryBackups, IntegrationCategoryDiskEncryption, IntegrationCategoryPatching:
		return true
	default:
		return false
	}
}

// IsValid reports whether s is one of the known coverage states.
func (s IntegrationState) IsValid() bool {
	switch s {
	case IntegrationStateProtected, IntegrationStateAtRisk, IntegrationStateNotInstalled, IntegrationStateUnknown:
		return true
	default:
		return false
	}
}

// HostIntegrationStatus is one community-plugin provider's coverage reading for a single host and
// category. It is a free/MIT feature: providers write these rows on their sync cadence and the host
// coverage matrix reads them. It does NOT require a premium license, and coexists with the
// enterprise layer.
type HostIntegrationStatus struct {
	HostID   uint                `json:"host_id" db:"host_id"`
	Source   string              `json:"source" db:"source"`
	Category IntegrationCategory `json:"category" db:"category"`
	State    IntegrationState    `json:"state" db:"state"`
	Detail   string              `json:"detail" db:"detail"`
	// UpdatedAt is when the provider last wrote this cell; used to compute staleness.
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
	// Stale is set at read time when the row is older than the category's freshness TTL; when true,
	// State is forced to "unknown" so the matrix never renders stale data as covered.
	Stale bool `json:"stale" db:"-"`
}

// CoverageStatePredicate is an exact (category, state) match used by CoverageFilter.
type CoverageStatePredicate struct {
	Category IntegrationCategory
	State    IntegrationState
}

// CoverageFilter selects hosts by their host_integration_status coverage, powering N-able-style saved
// views ("only hosts with problems", "only hosts missing Managed AV"). Set predicates are AND-combined.
// Freshness is applied in SQL (mirroring the read-path per-category TTL) so a stale "protected" cell is
// treated as a coverage gap, not as covered. Expectations for MissingCategories come from the host's
// bundle in the full design (human/RFC-coverage-dashboards-and-bundles.md §7).
type CoverageFilter struct {
	// Problems matches hosts with ANY effectively-non-protected cell (state != protected, OR a cell that
	// has gone stale past its category TTL).
	Problems bool
	// MissingCategories matches hosts lacking a fresh "protected" cell for EACH listed category.
	MissingCategories []IntegrationCategory
	// StatePredicates matches hosts having a fresh cell for EACH exact (category, state) pair. A state of
	// "unknown" also matches cells that are stale past their TTL.
	StatePredicates []CoverageStatePredicate
}

// IsZero reports whether the filter selects nothing (no coverage constraint set).
func (f CoverageFilter) IsZero() bool {
	return !f.Problems && len(f.MissingCategories) == 0 && len(f.StatePredicates) == 0
}

// AggregatedIntegrationStatus is a fleet-wide (optionally team-scoped) rollup of coverage cells,
// grouped by (source, category, state).
type AggregatedIntegrationStatus struct {
	Source   string              `json:"source" db:"source"`
	Category IntegrationCategory `json:"category" db:"category"`
	State    IntegrationState    `json:"state" db:"state"`
	Count    uint                `json:"count" db:"count"`
}
