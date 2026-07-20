package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/fleetdm/fleet/v4/server/mock"
	"github.com/stretchr/testify/require"
)

// TestCommunityProvidersConfigFromEnv pins the FLEET_COMMUNITY_* env-var spellings and the
// "configured" gate that decides whether the collector cron registers at all. A regression here
// silently disables the coverage collector in production (or registers a no-op cron), so each
// provider's minimal-cred gate is asserted explicitly.
func TestCommunityProvidersConfigFromEnv(t *testing.T) {
	// The vars this function reads; cleared per-case via t.Setenv so cases don't leak into each other.
	allVars := []string{
		"FLEET_COMMUNITY_SCREENCONNECT_URL", "FLEET_COMMUNITY_SCREENCONNECT_ACCESS_SECRET",
		"FLEET_COMMUNITY_SCREENCONNECT_API_PATH", "FLEET_COMMUNITY_SCREENCONNECT_INSTANCE_ID",
		"FLEET_COMMUNITY_SPLASHTOP_BASE_URL", "FLEET_COMMUNITY_SPLASHTOP_API_KEY", "FLEET_COMMUNITY_SPLASHTOP_COMPUTERS_PATH",
		"FLEET_COMMUNITY_BITDEFENDER_HOST", "FLEET_COMMUNITY_BITDEFENDER_API_KEY",
		"FLEET_COMMUNITY_ACTION1_BASE_URL", "FLEET_COMMUNITY_ACTION1_CLIENT_ID", "FLEET_COMMUNITY_ACTION1_CLIENT_SECRET",
		"FLEET_COMMUNITY_ACTION1_ORG_ID", "FLEET_COMMUNITY_ACTION1_AGENT_DOWNLOAD_ID",
	}

	cases := []struct {
		name         string
		env          map[string]string
		wantOK       bool
		checkConfig  func(t *testing.T)
		checkConfigF func(t *testing.T)
	}{
		{name: "nothing set", env: nil, wantOK: false},
		{name: "screenconnect url alone is enough (deployment-only mode)",
			env: map[string]string{"FLEET_COMMUNITY_SCREENCONNECT_URL": "https://remote.example.com"}, wantOK: true},
		{name: "splashtop api key alone is enough",
			env: map[string]string{"FLEET_COMMUNITY_SPLASHTOP_API_KEY": "sk-123"}, wantOK: true},
		{name: "bitdefender host without key is NOT configured",
			env: map[string]string{"FLEET_COMMUNITY_BITDEFENDER_HOST": "https://cloud.gravityzone.bitdefender.com"}, wantOK: false},
		{name: "bitdefender host + key is configured",
			env: map[string]string{
				"FLEET_COMMUNITY_BITDEFENDER_HOST":    "https://cloud.gravityzone.bitdefender.com",
				"FLEET_COMMUNITY_BITDEFENDER_API_KEY": "bd-key",
			}, wantOK: true},
		{name: "action1 client id+secret WITHOUT org id is NOT configured (would collect nothing)",
			env: map[string]string{
				"FLEET_COMMUNITY_ACTION1_CLIENT_ID":     "cid",
				"FLEET_COMMUNITY_ACTION1_CLIENT_SECRET": "csecret",
			}, wantOK: false},
		{name: "action1 client id+secret+org id is configured",
			env: map[string]string{
				"FLEET_COMMUNITY_ACTION1_CLIENT_ID":     "cid",
				"FLEET_COMMUNITY_ACTION1_CLIENT_SECRET": "csecret",
				"FLEET_COMMUNITY_ACTION1_ORG_ID":        "org1",
			}, wantOK: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, v := range allVars {
				t.Setenv(v, "")
			}
			for k, v := range c.env {
				t.Setenv(k, v)
			}

			cfg, ok := communityProvidersConfigFromEnv()
			require.Equal(t, c.wantOK, ok)

			// Values must land in the right Config fields (pins the exact env-var spellings).
			for k, v := range c.env {
				switch k {
				case "FLEET_COMMUNITY_SCREENCONNECT_URL":
					require.Equal(t, v, cfg.ScreenConnect.InstanceURL)
				case "FLEET_COMMUNITY_SPLASHTOP_API_KEY":
					require.Equal(t, v, cfg.Splashtop.APIKey)
				case "FLEET_COMMUNITY_BITDEFENDER_HOST":
					require.Equal(t, v, cfg.Bitdefender.Host)
				case "FLEET_COMMUNITY_BITDEFENDER_API_KEY":
					require.Equal(t, v, cfg.Bitdefender.APIKey)
				case "FLEET_COMMUNITY_ACTION1_CLIENT_ID":
					require.Equal(t, v, cfg.Action1.ClientID)
				case "FLEET_COMMUNITY_ACTION1_ORG_ID":
					require.Equal(t, v, cfg.Action1.OrgID)
				}
			}
		})
	}
}

// TestNewCommunityHostStatusScheduleValidatesConfig confirms the constructor builds a schedule for a
// good config and returns an error for a malformed configured ScreenConnect URL. The error path is what
// registerMiscCrons logs-and-skips (non-fatally), so a typo disables the collector without taking the
// whole server down.
func TestNewCommunityHostStatusScheduleValidatesConfig(t *testing.T) {
	ctx := context.Background()
	ds := new(mock.Store)
	logger := slog.New(slog.DiscardHandler)

	t.Run("valid splashtop-only config builds a schedule", func(t *testing.T) {
		for _, v := range []string{"FLEET_COMMUNITY_SCREENCONNECT_URL", "FLEET_COMMUNITY_SPLASHTOP_API_KEY"} {
			t.Setenv(v, "")
		}
		t.Setenv("FLEET_COMMUNITY_SPLASHTOP_API_KEY", "sk-123")
		cfg, ok := communityProvidersConfigFromEnv()
		require.True(t, ok)
		sch, err := newCommunityHostStatusSchedule(ctx, "test-instance", ds, logger, cfg)
		require.NoError(t, err)
		require.NotNil(t, sch)
	})

	t.Run("malformed screenconnect url returns an error (registration skips, not fatal)", func(t *testing.T) {
		t.Setenv("FLEET_COMMUNITY_SCREENCONNECT_URL", "://not-a-url")
		cfg, ok := communityProvidersConfigFromEnv()
		require.True(t, ok)
		_, err := newCommunityHostStatusSchedule(ctx, "test-instance", ds, logger, cfg)
		require.Error(t, err)
	})
}
