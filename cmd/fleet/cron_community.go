package main

// Fork-only: the community host-status collector's cron wiring lives in this file (not cron.go, the
// hottest-churn core file) so the fork's rebase surface against upstream stays confined to
// community-owned files. See server/community/ for the providers and runner.

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/fleetdm/fleet/v4/server/community"
	"github.com/fleetdm/fleet/v4/server/community/action1"
	"github.com/fleetdm/fleet/v4/server/community/bitdefender"
	"github.com/fleetdm/fleet/v4/server/community/providers"
	"github.com/fleetdm/fleet/v4/server/community/screenconnect"
	"github.com/fleetdm/fleet/v4/server/community/splashtop"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/fleetdm/fleet/v4/server/service/schedule"
)

// communityProvidersConfigFromEnv assembles the community host-status provider config from FLEET_COMMUNITY_*
// environment variables. It returns (cfg, true) when at least one provider is configured, or (zero, false)
// when nothing is set — the caller then skips registering the collector schedule so a stock deployment never
// runs a no-op cron. Secrets are read from env for the MVP; a later revision moves them into app_config
// (envelope-encrypted, per human/RISK-REGISTER.md). See human/setup/*.
func communityProvidersConfigFromEnv() (providers.Config, bool) {
	env := os.Getenv
	cfg := providers.Config{
		ScreenConnect: screenconnect.Config{
			InstanceURL:   env("FLEET_COMMUNITY_SCREENCONNECT_URL"),
			AccessSecret:  env("FLEET_COMMUNITY_SCREENCONNECT_ACCESS_SECRET"),
			APIPath:       env("FLEET_COMMUNITY_SCREENCONNECT_API_PATH"),
			SessionFilter: env("FLEET_COMMUNITY_SCREENCONNECT_SESSION_FILTER"),
			InstanceID:    env("FLEET_COMMUNITY_SCREENCONNECT_INSTANCE_ID"),
		},
		Splashtop: splashtop.Config{
			BaseURL:       env("FLEET_COMMUNITY_SPLASHTOP_BASE_URL"),
			APIKey:        env("FLEET_COMMUNITY_SPLASHTOP_API_KEY"),
			ComputersPath: env("FLEET_COMMUNITY_SPLASHTOP_COMPUTERS_PATH"),
		},
		Bitdefender: bitdefender.Config{
			Host:   env("FLEET_COMMUNITY_BITDEFENDER_HOST"),
			APIKey: env("FLEET_COMMUNITY_BITDEFENDER_API_KEY"),
		},
		Action1: action1.Config{
			BaseURL:         env("FLEET_COMMUNITY_ACTION1_BASE_URL"),
			ClientID:        env("FLEET_COMMUNITY_ACTION1_CLIENT_ID"),
			ClientSecret:    env("FLEET_COMMUNITY_ACTION1_CLIENT_SECRET"),
			OrgID:           env("FLEET_COMMUNITY_ACTION1_ORG_ID"),
			AgentDownloadID: env("FLEET_COMMUNITY_ACTION1_AGENT_DOWNLOAD_ID"),
		},
	}
	// Each provider counts as configured only when it has the MINIMUM creds its Collect actually needs —
	// otherwise the collector registers but that provider silently no-ops. ScreenConnect needs only the URL
	// (deployment-only mode is valid; polling additionally needs AccessSecret+APIPath). Action1 needs
	// OrgID too — action1.Collect returns nothing without it, so ID+secret alone must NOT count as
	// configured (that shipped a cron that collected nothing with no signal).
	configured := cfg.ScreenConnect.InstanceURL != "" ||
		cfg.Splashtop.APIKey != "" ||
		(cfg.Bitdefender.Host != "" && cfg.Bitdefender.APIKey != "") ||
		(cfg.Action1.ClientID != "" && cfg.Action1.ClientSecret != "" && cfg.Action1.OrgID != "")
	return cfg, configured
}

// newCommunityHostStatusSchedule builds the fork-only community host-status collector schedule: it registers
// the configured providers and runs community.Runner.Run every 5 minutes to upsert host_integration_status
// coverage cells (updated_at is bumped on every successful write, which is what drives the staleness/coverage
// dashboard). Registration validates provider config — a malformed configured ScreenConnect URL returns an
// error here, which registerMiscCrons logs and skips (the collector is optional and must not abort boot).
// NOTE: Runner.Run currently logs and swallows per-provider failures; surfacing a failed poll
// (per-provider health) is the next step in the observability design.
func newCommunityHostStatusSchedule(
	ctx context.Context,
	instanceID string,
	ds fleet.Datastore,
	logger *slog.Logger,
	cfg providers.Config,
) (*schedule.Schedule, error) {
	const (
		name     = string(fleet.CronCommunityHostStatus)
		interval = 5 * time.Minute
	)
	logger = logger.With("cron", name)

	reg := community.NewRegistry()
	if err := providers.Register(reg, cfg); err != nil {
		return nil, err
	}
	runner := community.NewRunner(reg, ds, logger)

	s := schedule.New(
		ctx, name, instanceID, interval, ds, ds,
		schedule.WithLogger(logger),
		schedule.WithJob("community_host_status", func(ctx context.Context) error {
			return runner.Run(ctx)
		}),
	)
	return s, nil
}

// registerCommunityCrons registers the fork-only community collector, called from registerMiscCrons via a
// single one-line hook. Registered only when at least one FLEET_COMMUNITY_* provider is configured, so
// stock deployments don't run a no-op cron. A misconfigured provider (e.g. a malformed ScreenConnect URL)
// disables the coverage collector but must NOT abort server boot the way deps.register/initFatal would for
// a core schedule — fail loudly in the log and keep the server (and every other schedule) running.
func registerCommunityCrons(ctx context.Context, deps cronSchedulesDeps) {
	cfg, ok := communityProvidersConfigFromEnv()
	if !ok {
		deps.logger.InfoContext(ctx, "community host-status collector not configured; skipping (set FLEET_COMMUNITY_* env vars to enable)")
		return
	}
	if err := deps.cronSchedules.StartCronSchedule(func() (fleet.CronSchedule, error) {
		return newCommunityHostStatusSchedule(ctx, deps.instanceID, deps.ds, deps.logger, cfg)
	}); err != nil {
		deps.logger.ErrorContext(ctx, "community host-status collector misconfigured; coverage collection disabled (fix FLEET_COMMUNITY_* env and restart)", "err", err)
	}
}
