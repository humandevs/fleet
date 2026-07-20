package action1

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/stretchr/testify/require"
)

func TestCollectReportsPatchPosture(t *testing.T) {
	recent := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	old := time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)

	var tokenBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			b, _ := io.ReadAll(r.Body)
			tokenBody = string(b)
			_, _ = w.Write([]byte(`{"access_token":"JWT123","expires_in":3600,"token_type":"bearer"}`))
		case r.URL.Path == "/endpoints/managed/org-1":
			require.Equal(t, "Bearer JWT123", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(fmt.Sprintf(`{"items":[
				{"id":"a","name":"HOST-A","last_seen":"%s"},
				{"id":"b","name":"HOST-B","last_seen":"%s"},
				{"id":"c","name":"HOST-C","last_seen":"%s"}
			]}`, recent, recent, old)))
		case r.URL.Path == "/updates/org-1":
			_, _ = w.Write([]byte(`{"items":[{"endpoint_id":"b"},{"endpoint_id":"b"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, ClientID: "cid", ClientSecret: "sec", OrgID: "org-1"})
	reports, err := p.Collect(t.Context())
	require.NoError(t, err)

	// Client-credentials with NO grant_type (a generic OAuth2 lib would send one).
	require.Contains(t, tokenBody, "client_id=cid")
	require.NotContains(t, tokenBody, "grant_type")

	states := map[string]fleet.IntegrationState{}
	for _, r := range reports {
		require.Equal(t, fleet.IntegrationCategoryPatching, r.Category)
		states[r.Identifier] = r.State
	}
	require.Equal(t, fleet.IntegrationStateProtected, states["HOST-A"]) // recent + no missing
	require.Equal(t, fleet.IntegrationStateAtRisk, states["HOST-B"])    // 2 missing updates
	require.Equal(t, fleet.IntegrationStateAtRisk, states["HOST-C"])    // stale agent
}

func TestWindowsInstallScript(t *testing.T) {
	p := New(Config{AgentDownloadID: "abc-123"})
	s := p.WindowsInstallScript()
	require.Contains(t, s, "https://app.action1.com/agent/abc-123/Windows/agent.msi")
	require.Contains(t, s, "msiexec.exe /i")
}

func TestCollectNoopWithoutConfig(t *testing.T) {
	reports, err := New(Config{}).Collect(t.Context())
	require.NoError(t, err)
	require.Empty(t, reports)
}

func TestPatchStateStaleAgent(t *testing.T) {
	old := time.Now().Add(-10 * 24 * time.Hour)
	state, detail := patchState(managedEndpoint{Name: "H", LastSeen: &old}, 0, defaultStaleAfter)
	require.Equal(t, fleet.IntegrationStateAtRisk, state)
	require.Contains(t, detail, "no check-in")
}

func TestPatchStateNeverReported(t *testing.T) {
	state, detail := patchState(managedEndpoint{Name: "H"}, 0, defaultStaleAfter)
	require.Equal(t, fleet.IntegrationStateAtRisk, state)
	require.Equal(t, "agent never reported", detail)
}

func TestManagedEndpointsPaginates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_, _ = w.Write([]byte(`{"access_token":"JWT123","expires_in":3600,"token_type":"bearer"}`))
		case "/endpoints/managed/org-1":
			switch r.URL.Query().Get("from") {
			case "0":
				_, _ = w.Write([]byte(fmt.Sprintf(`{"items":[
					{"id":"a","name":"HOST-A"},
					{"id":"b","name":"HOST-B"}
				],"next_page":"%s/endpoints/managed/org-1?from=2"}`, r.Host)))
			case "2":
				_, _ = w.Write([]byte(`{"items":[{"id":"c","name":"HOST-C"}]}`))
			default:
				t.Errorf("unexpected from=%q", r.URL.Query().Get("from"))
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, ClientID: "cid", ClientSecret: "sec", OrgID: "org-1"})
	endpoints, err := p.managedEndpoints(t.Context())
	require.NoError(t, err)

	names := make([]string, 0, len(endpoints))
	for _, e := range endpoints {
		names = append(names, e.Name)
	}
	require.Equal(t, []string{"HOST-A", "HOST-B", "HOST-C"}, names)
}

func TestMissingUpdatesPaginates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_, _ = w.Write([]byte(`{"access_token":"JWT123","expires_in":3600,"token_type":"bearer"}`))
		case "/updates/org-1":
			switch r.URL.Query().Get("from") {
			case "0":
				_, _ = w.Write([]byte(fmt.Sprintf(`{"items":[
					{"endpoint_id":"a"},
					{"endpoint_id":"b"}
				],"next_page":"%s/updates/org-1?from=2"}`, r.Host)))
			case "2":
				_, _ = w.Write([]byte(`{"items":[{"endpoint_id":"b"}]}`))
			default:
				t.Errorf("unexpected from=%q", r.URL.Query().Get("from"))
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, ClientID: "cid", ClientSecret: "sec", OrgID: "org-1"})
	counts, err := p.missingUpdateCounts(t.Context())
	require.NoError(t, err)
	require.Equal(t, map[string]int{"a": 1, "b": 2}, counts)
}

func TestTokenCachedAcrossCalls(t *testing.T) {
	tokenMints := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			tokenMints++
			_, _ = w.Write([]byte(`{"access_token":"JWT123","expires_in":3600,"token_type":"bearer"}`))
		case "/endpoints/managed/org-1", "/updates/org-1":
			require.Equal(t, "Bearer JWT123", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, ClientID: "cid", ClientSecret: "sec", OrgID: "org-1"})
	for range 2 {
		_, err := p.Collect(t.Context())
		require.NoError(t, err)
	}
	require.Equal(t, 1, tokenMints, "second Collect must reuse the cached token")
}
