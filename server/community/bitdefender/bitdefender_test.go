package bitdefender

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fleetdm/fleet/v4/server/community"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/stretchr/testify/require"
)

func TestCollectMapsEndpointsToAVandMDR(t *testing.T) {
	var authSeen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authSeen = r.Header.Get("Authorization")
		var req rpcRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/network") && req.Method == "getEndpointsList":
			_, _ = w.Write([]byte(`{"result":{"page":1,"pagesCount":1,"total":2,"items":[{"id":"e1","name":"DESKTOP-01"},{"id":"e2","name":"DESKTOP-02"}]}}`))
		case req.Method == "getManagedEndpointDetails":
			id := req.Params.(map[string]any)["endpointId"].(string)
			if id == "e1" {
				// Healthy AV + EDR.
				_, _ = w.Write([]byte(`{"result":{"name":"DESKTOP-01","agent":{"productOutdated":false},"malwareStatus":{"infected":false},"modules":{"antimalware":true,"edrSensor":true}}}`))
			} else {
				// AV module off, no EDR.
				_, _ = w.Write([]byte(`{"result":{"name":"DESKTOP-02","agent":{"productOutdated":false},"malwareStatus":{"infected":false},"modules":{"antimalware":false,"edrSensor":false}}}`))
			}
		default:
			t.Fatalf("unexpected call: %s %s", r.URL.Path, req.Method)
		}
	}))
	defer srv.Close()

	p := New(Config{Host: srv.URL, APIKey: "KEY123"})
	reports, err := p.Collect(t.Context())
	require.NoError(t, err)

	// Basic auth: username = API key, empty password → base64("KEY123:").
	require.Equal(t, "Basic S0VZMTIzOg==", authSeen)

	// 2 endpoints × (av + mdr) = 4 reports.
	require.Len(t, reports, 4)
	got := map[string]fleet.IntegrationState{}
	for _, r := range reports {
		require.Equal(t, community.IdentifierHostname, r.IdentifierKind)
		got[r.Identifier+"/"+string(r.Category)] = r.State
	}
	require.Equal(t, fleet.IntegrationStateProtected, got["DESKTOP-01/av"])
	require.Equal(t, fleet.IntegrationStateProtected, got["DESKTOP-01/mdr"])
	require.Equal(t, fleet.IntegrationStateNotInstalled, got["DESKTOP-02/av"])
	require.Equal(t, fleet.IntegrationStateNotInstalled, got["DESKTOP-02/mdr"])
}

func TestCollectSurfacesRPCError(t *testing.T) {
	// JSON-RPC returns HTTP 200 even on error — the body's error field must be honored.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":{"code":-32600,"message":"invalid key"}}`))
	}))
	defer srv.Close()

	_, err := New(Config{Host: srv.URL, APIKey: "bad"}).Collect(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid key")
}

func TestCollectNoopWithoutConfig(t *testing.T) {
	reports, err := New(Config{}).Collect(t.Context())
	require.NoError(t, err)
	require.Empty(t, reports)
}

func TestMalwareDetectionIsAtRisk(t *testing.T) {
	// Antimalware on but active detection → at_risk (not protected).
	var d endpointDetails
	require.NoError(t, json.Unmarshal([]byte(`{"modules":{"antimalware":true},"malwareStatus":{"infected":true}}`), &d))
	s, _, _, _ := mapStates(d)
	require.Equal(t, fleet.IntegrationStateAtRisk, s)
}
