package screenconnect

import (
	"strings"
	"testing"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/stretchr/testify/require"
)

func TestInstallerURLLinksOrg(t *testing.T) {
	p := New(Config{InstanceURL: "https://example.screenconnect.com/"})
	got := p.InstallerURL("DESKTOP-01", Org("Acme Inc", "HQ"))
	// The two c= properties (CustomProperty1=client, CustomProperty2=site) route the device into its group.
	require.Equal(t,
		"https://example.screenconnect.com/Bin/ScreenConnect.ClientSetup.msi?e=Access&y=Guest&t=DESKTOP-01&c=Acme+Inc&c=HQ",
		got,
	)
}

func TestInstallerURLCustomProperties(t *testing.T) {
	p := New(Config{InstanceURL: "https://x"})

	// Full positional CustomProperty1..4: client, site, department, device type.
	got := p.InstallerURL("h1", OrgLink{CustomProperties: []string{"Acme", "HQ", "IT", "Laptop"}})
	require.Contains(t, got, "&c=Acme&c=HQ&c=IT&c=Laptop")

	// Trailing empties trimmed; interior empty preserved (c= is positional, so CustomProperty2 stays blank).
	got = p.InstallerURL("h1", OrgLink{CustomProperties: []string{"Acme", "", "IT", "", ""}})
	require.True(t, strings.HasSuffix(got, "&c=Acme&c=&c=IT"), got)

	// Capped at ScreenConnect's 8.
	got = p.InstallerURL("h1", OrgLink{CustomProperties: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}})
	require.Contains(t, got, "&c=8")
	require.NotContains(t, got, "&c=9")
}

func TestEnsureSessionGroupSeam(t *testing.T) {
	// Provisioning seam is callable today (no-op) so the Fleet client/site lifecycle hook can wire to it.
	p := New(Config{InstanceURL: "https://x"})
	require.NoError(t, p.EnsureSessionGroup(t.Context(), "Acme", "HQ", "Remote"))
}

func TestInstallerURLSelfHostedRelay(t *testing.T) {
	// Self-hosted, reverse-proxied: web UI on 443, relay on host:8041, self-signed cert. The h=/p=/k=
	// overrides point the agent at the real relay endpoint instead of the proxy baked into the download.
	p := New(Config{
		InstanceURL: "https://remote.example.com",
		RelayHost:   "relay.example.com",
		RelayPort:   8041,
		Thumbprint:  "AA:BB:CC",
	})
	got := p.InstallerURL("DESKTOP-01", Org("Acme Inc", "HQ"))
	require.Equal(t,
		"https://remote.example.com/Bin/ScreenConnect.ClientSetup.msi?e=Access&y=Guest&t=DESKTOP-01&c=Acme+Inc&c=HQ&h=relay.example.com&p=8041&k=AA%3ABB%3ACC",
		got,
	)
}

func TestValidate(t *testing.T) {
	require.NoError(t, Config{InstanceURL: "https://remote.example.com:8040"}.Validate())
	// Self-hosted has no default domain — empty or non-absolute URLs must fail loudly.
	require.Error(t, Config{}.Validate())
	require.Error(t, Config{InstanceURL: "remote.example.com"}.Validate())
	require.Error(t, Config{InstanceURL: "ftp://remote.example.com"}.Validate())
}

func TestProviderContract(t *testing.T) {
	p := New(Config{InstanceURL: "https://x"})
	require.Equal(t, "screenconnect", p.Source())
	require.Equal(t, []fleet.IntegrationCategory{fleet.IntegrationCategoryRemoteAccess}, p.Categories())
	require.Contains(t, p.WindowsInstallScript("h1", Org("C", "S")), "msiexec.exe /i")
}
