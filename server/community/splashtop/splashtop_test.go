package splashtop

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fleetdm/fleet/v4/server/community"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/stretchr/testify/require"
)

func TestProviderContract(t *testing.T) {
	p := New(Config{})
	require.Equal(t, "splashtop", p.Source())
	require.Equal(t, []fleet.IntegrationCategory{fleet.IntegrationCategoryRemoteAccess}, p.Categories())
}

func TestCollectMapsComputersToRemoteAccess(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"computers":[
			{"id":"1","name":"DESKTOP-01","online":true},
			{"id":"2","name":"DESKTOP-02","online":false},
			{"id":"3","online":true}
		]}`))
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, APIKey: "k3y"})
	reports, err := p.Collect(t.Context())
	require.NoError(t, err)

	require.Equal(t, "Bearer k3y", gotAuth)
	require.Equal(t, "/v1/computers", gotPath)

	// The unnamed computer (id 3) is dropped — no host key to resolve. The other two map by hostname.
	require.Len(t, reports, 2)
	for _, r := range reports {
		require.Equal(t, community.IdentifierHostname, r.IdentifierKind)
		require.Equal(t, fleet.IntegrationCategoryRemoteAccess, r.Category)
	}
	require.Equal(t, "DESKTOP-01", reports[0].Identifier)
	require.Equal(t, fleet.IntegrationStateProtected, reports[0].State) // online
	require.Equal(t, "DESKTOP-02", reports[1].Identifier)
	require.Equal(t, fleet.IntegrationStateAtRisk, reports[1].State) // known but offline
}

func TestCollectAcceptsBareArray(t *testing.T) {
	// Some plans/versions return a bare array instead of a {"computers":[...]} wrapper — both must parse.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"1","name":"HOST-A","online":true}]`))
	}))
	defer srv.Close()

	reports, err := New(Config{BaseURL: srv.URL, APIKey: "k"}).Collect(t.Context())
	require.NoError(t, err)
	require.Len(t, reports, 1)
	require.Equal(t, "HOST-A", reports[0].Identifier)
	require.Equal(t, fleet.IntegrationStateProtected, reports[0].State)
}

func TestCollectEmptyComputersList(t *testing.T) {
	// A wrapper that decodes successfully but carries an empty (or null, or absent) list is a legitimate
	// empty tenant — zero reports, no error. It must NOT fall through to a bare-array parse of the object,
	// which would misreport it as a decode error.
	for _, tc := range []struct {
		name string
		body string
	}{
		{"empty computers", `{"computers":[]}`},
		{"null computers", `{"computers":null}`},
		{"empty items", `{"items":[]}`},
		{"empty object", `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			reports, err := New(Config{BaseURL: srv.URL, APIKey: "k"}).Collect(t.Context())
			require.NoError(t, err)
			require.Empty(t, reports)
		})
	}
}

func TestCollectAcceptsItemsWrapper(t *testing.T) {
	// Some plans/versions wrap the list as {"items":[...]} instead of {"computers":[...]} — both must parse.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"1","name":"HOST-B","online":false}]}`))
	}))
	defer srv.Close()

	reports, err := New(Config{BaseURL: srv.URL, APIKey: "k"}).Collect(t.Context())
	require.NoError(t, err)
	require.Len(t, reports, 1)
	require.Equal(t, "HOST-B", reports[0].Identifier)
	require.Equal(t, fleet.IntegrationStateAtRisk, reports[0].State) // known but offline
}

func TestCollectOversizedResponseErrors(t *testing.T) {
	// A spoofed/malicious server streaming an unbounded body must hit the maxResponseBytes cap and
	// error, not be buffered without limit. Content is irrelevant — the size check fires before decode.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chunk := bytes.Repeat([]byte("x"), 1<<20) // 1 MB
		for written := 0; written <= maxResponseBytes; written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return // client stopped reading at the cap
			}
		}
	}))
	defer srv.Close()

	reports, err := New(Config{BaseURL: srv.URL, APIKey: "k"}).Collect(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")
	require.Nil(t, reports)
}

func TestCollectNoopWithoutAPIKey(t *testing.T) {
	// Unconfigured (no API key) is valid: Collect returns nothing and makes no HTTP call.
	reports, err := New(Config{}).Collect(t.Context())
	require.NoError(t, err)
	require.Empty(t, reports)
}

func TestDefaultsApplied(t *testing.T) {
	p := New(Config{APIKey: "k"})
	require.Equal(t, defaultBaseURL, p.cfg.BaseURL)
	require.Equal(t, defaultComputersPath, p.cfg.ComputersPath)
}

func TestCollectNon200Errors(t *testing.T) {
	// A failing list-computers call must surface as an error, never an empty computer list — which the
	// coverage matrix would misread as "no hosts covered" (same invariant screenconnect pins).
	for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			reports, err := New(Config{BaseURL: srv.URL, APIKey: "k"}).Collect(t.Context())
			require.Error(t, err)
			require.Contains(t, err.Error(), strconv.Itoa(status))
			require.Nil(t, reports)
		})
	}
}
