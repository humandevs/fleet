package screenconnect

import (
	"testing"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/stretchr/testify/require"
)

func TestInstallerURLLinksOrg(t *testing.T) {
	p := New(Config{InstanceURL: "https://example.screenconnect.com/"})
	got := p.InstallerURL("DESKTOP-01", OrgLink{Company: "Acme Inc", Site: "HQ"})
	// The two c= properties (Company, Site) are what link the device to the org on install.
	require.Equal(t,
		"https://example.screenconnect.com/Bin/ScreenConnect.ClientSetup.msi?e=Access&y=Guest&t=DESKTOP-01&c=Acme+Inc&c=HQ",
		got,
	)
}

func TestProviderContract(t *testing.T) {
	p := New(Config{InstanceURL: "https://x"})
	require.Equal(t, "screenconnect", p.Source())
	require.Equal(t, []fleet.IntegrationCategory{fleet.IntegrationCategoryRemoteAccess}, p.Categories())
	require.Contains(t, p.WindowsInstallScript("h1", OrgLink{Company: "C", Site: "S"}), "msiexec.exe /i")
}
