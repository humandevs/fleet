package bitdefender

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestListEndpointsPaginates(t *testing.T) {
	var pagesRequested []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		w.Header().Set("Content-Type", "application/json")

		switch req.Method {
		case "getEndpointsList":
			page := int(req.Params.(map[string]any)["page"].(float64))
			pagesRequested = append(pagesRequested, page)
			if page == 1 {
				_, _ = w.Write([]byte(`{"result":{"page":1,"pagesCount":2,"total":2,"items":[{"id":"e1","name":"PAGE1-HOST"}]}}`))
			} else {
				_, _ = w.Write([]byte(`{"result":{"page":2,"pagesCount":2,"total":2,"items":[{"id":"e2","name":"PAGE2-HOST"}]}}`))
			}
		case "getManagedEndpointDetails":
			id := req.Params.(map[string]any)["endpointId"].(string)
			name := map[string]string{"e1": "PAGE1-HOST", "e2": "PAGE2-HOST"}[id]
			_, _ = w.Write([]byte(`{"result":{"name":"` + name + `","modules":{"antimalware":true}}}`))
		default:
			t.Errorf("unexpected method: %s", req.Method)
		}
	}))
	defer srv.Close()

	reports, err := New(Config{Host: srv.URL, APIKey: "k"}).Collect(t.Context())
	require.NoError(t, err)

	// Page 1 then page 2 were requested, and endpoints from BOTH pages yield reports.
	require.Equal(t, []int{1, 2}, pagesRequested)
	require.Len(t, reports, 4) // 2 endpoints × (av + mdr)
	names := map[string]bool{}
	for _, r := range reports {
		names[r.Identifier] = true
	}
	require.True(t, names["PAGE1-HOST"])
	require.True(t, names["PAGE2-HOST"])
}

func TestOneBadEndpointDoesNotAbortSweep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		w.Header().Set("Content-Type", "application/json")

		switch req.Method {
		case "getEndpointsList":
			_, _ = w.Write([]byte(`{"result":{"page":1,"pagesCount":1,"total":3,"items":[{"id":"e1","name":"OK-1"},{"id":"e2","name":"BAD"},{"id":"e3","name":"OK-2"}]}}`))
		case "getManagedEndpointDetails":
			id := req.Params.(map[string]any)["endpointId"].(string)
			if id == "e2" {
				// JSON-RPC error for one endpoint only — the sweep must skip it, not abort.
				_, _ = w.Write([]byte(`{"error":{"code":-32000,"message":"endpoint not found"}}`))
				return
			}
			name := map[string]string{"e1": "OK-1", "e3": "OK-2"}[id]
			_, _ = w.Write([]byte(`{"result":{"name":"` + name + `","modules":{"antimalware":true}}}`))
		default:
			t.Errorf("unexpected method: %s", req.Method)
		}
	}))
	defer srv.Close()

	p := New(Config{Host: srv.URL, APIKey: "k"})
	p.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	reports, err := p.Collect(t.Context())
	require.NoError(t, err)

	// The two healthy endpoints still report (av + mdr each); the bad one is skipped.
	require.Len(t, reports, 4)
	for _, r := range reports {
		require.NotEqual(t, "BAD", r.Identifier)
	}
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

func TestOutdatedAgentIsAtRisk(t *testing.T) {
	// Antimalware on, no detection, but the agent itself is outdated → at_risk.
	var d endpointDetails
	require.NoError(t, json.Unmarshal([]byte(`{"modules":{"antimalware":true},"agent":{"productOutdated":true}}`), &d))
	s, detail, _, _ := mapStates(d)
	require.Equal(t, fleet.IntegrationStateAtRisk, s)
	require.Equal(t, "agent outdated", detail)
}

func TestCollectNon200Errors(t *testing.T) {
	// A non-200 from GravityZone must surface as an error (a wrong region silently 401s — see Config.Host),
	// never an empty endpoint list the coverage matrix would misread as "no hosts covered".
	for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			reports, err := New(Config{Host: srv.URL, APIKey: "k"}).Collect(t.Context())
			require.Error(t, err)
			require.Contains(t, err.Error(), strconv.Itoa(status))
			require.Nil(t, reports)
		})
	}
}
