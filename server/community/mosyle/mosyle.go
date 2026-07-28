// Package mosyle scaffolds the Mosyle integration. Mosyle is the primary Apple MDM authority and a
// device-ingestion source — NOT a coverage-matrix HostStatusProvider — so it is modeled as its own
// plugin shape (ingest Mosyle-managed devices as hosts; delegate Apple MDM actions). Mock. See
// human/setup/mosyle.md and human/OSS.md §8.4.
package mosyle

// Provider is a mock scaffold for the Mosyle integration (Apple MDM authority + device ingestion).
type Provider struct{}

func New() *Provider { return &Provider{} }

// Source is the stable provider key.
func (p *Provider) Source() string { return "mosyle" }
