package screenconnect

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fleetdm/fleet/v4/server/community"
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

func TestCollectMapsSessionsToRemoteAccess(t *testing.T) {
	var gotSecret, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecret = r.Header.Get("CTRLAuthHeader")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"SessionID":"g1","Name":"DESKTOP-01","GuestConnectedCount":1},
			{"SessionID":"g2","Name":"DESKTOP-02","GuestConnectedCount":0},
			{"SessionID":"g3","GuestMachineName":"DESKTOP-03","GuestConnectedCount":0},
			{"SessionID":"g4","GuestConnectedCount":1}
		]`))
	}))
	defer srv.Close()

	p := New(Config{
		InstanceURL:  srv.URL,
		AccessSecret: "s3cr3t",
		APIPath:      "/App_Extensions/abc/Service.ashx/GetSessionsByFilter",
	})
	reports, err := p.Collect(t.Context())
	require.NoError(t, err)

	require.Equal(t, "s3cr3t", gotSecret)
	require.Equal(t, "/App_Extensions/abc/Service.ashx/GetSessionsByFilter", gotPath)

	// g4 has no name/machine-name → dropped (no host key). The other three map by hostname.
	require.Len(t, reports, 3)
	for _, r := range reports {
		require.Equal(t, community.IdentifierHostname, r.IdentifierKind)
		require.Equal(t, fleet.IntegrationCategoryRemoteAccess, r.Category)
	}
	require.Equal(t, "DESKTOP-01", reports[0].Identifier)
	require.Equal(t, fleet.IntegrationStateProtected, reports[0].State) // connected guest
	require.Equal(t, "DESKTOP-02", reports[1].Identifier)
	require.Equal(t, fleet.IntegrationStateAtRisk, reports[1].State) // known but offline
	require.Equal(t, "DESKTOP-03", reports[2].Identifier)            // falls back to GuestMachineName
}

func TestCollectNon200Errors(t *testing.T) {
	// A rejected or failing RESTful API Manager call must surface as an error — never as an empty
	// session list, which the coverage matrix would misread as "no hosts covered".
	for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			p := New(Config{
				InstanceURL:  srv.URL,
				AccessSecret: "s3cr3t",
				APIPath:      "/App_Extensions/abc/Service.ashx/GetSessionsByFilter",
			})
			reports, err := p.Collect(t.Context())
			require.Error(t, err)
			require.Contains(t, err.Error(), strconv.Itoa(status))
			require.Nil(t, reports)
		})
	}
}

func TestCollectMalformedJSONErrors(t *testing.T) {
	// A body that isn't a session array must error, not silently yield zero reports.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"not":"a session array"`))
	}))
	defer srv.Close()

	p := New(Config{
		InstanceURL:  srv.URL,
		AccessSecret: "s3cr3t",
		APIPath:      "/App_Extensions/abc/Service.ashx/GetSessionsByFilter",
	})
	reports, err := p.Collect(t.Context())
	require.Error(t, err)
	require.Nil(t, reports)
}

func TestCollectOversizedResponseErrors(t *testing.T) {
	// A spoofed/malicious server streaming an unbounded body must hit the maxResponseBytes cap and
	// error, not be buffered without limit. Content is irrelevant — the size check fires before decode.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := bytes.Repeat([]byte("x"), 1<<20) // 1 MB
		for written := 0; written <= maxResponseBytes; written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return // client stopped reading at the cap
			}
		}
	}))
	defer srv.Close()

	p := New(Config{
		InstanceURL:  srv.URL,
		AccessSecret: "s3cr3t",
		APIPath:      "/App_Extensions/abc/Service.ashx/GetSessionsByFilter",
	})
	reports, err := p.Collect(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")
	require.Nil(t, reports)
}

func TestCollectNoopWithoutPolling(t *testing.T) {
	// Deployment-only instance (no API secret/path) is valid: Collect returns nothing, no HTTP call.
	reports, err := New(Config{InstanceURL: "https://x"}).Collect(t.Context())
	require.NoError(t, err)
	require.Empty(t, reports)
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

func TestPresenceQueryScopedToOurInstance(t *testing.T) {
	// With our InstanceID set, presence matches ONLY our service name — a side-by-side competitor's
	// "ScreenConnect Client (<their id>)" does not satisfy this query.
	p := New(Config{InstanceURL: "https://x", InstanceID: "a1b2c3d4e5f6a7b8"})
	require.Equal(t, "ScreenConnect Client (a1b2c3d4e5f6a7b8)", p.ServiceName())
	require.Equal(t,
		"SELECT 1 FROM services WHERE name = 'ScreenConnect Client (a1b2c3d4e5f6a7b8)' AND status = 'RUNNING';",
		p.PresenceQuery(),
	)

	// Without an InstanceID, local presence detection is disabled (can't tell our agent from a competitor's).
	none := New(Config{InstanceURL: "https://x"})
	require.Empty(t, none.ServiceName())
	require.Empty(t, none.PresenceQuery())
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
