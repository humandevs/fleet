// Package action1 scaffolds the Action1 integration. Action1 is a patch-management control-plane +
// Windows agent-deploy plugin — NOT a coverage-matrix HostStatusProvider (it maps to no av/mdr/
// remote_access/backups/disk_encryption cell), so it is modeled as its own plugin shape. Mock. See
// human/setup/action1.md.
package action1

// Provider is a mock scaffold for the Action1 integration (patch management + agent deployment).
type Provider struct{}

func New() *Provider { return &Provider{} }

// Source is the stable provider key.
func (p *Provider) Source() string { return "action1" }
