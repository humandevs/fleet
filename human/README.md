# `human/`

Company namespace for our FleetDM fork — strategy, architecture decisions, and fork-specific
documentation that is **not** part of upstream Fleet.

This folder holds **docs**, not code. Integration and rebuilt-feature **code** lives in idiomatic
MIT packages under `server/` (e.g. `server/integrations/`, `server/endpointprotection/`,
`server/mdm/mosyle/`) — Fleet has no plugin architecture, so a `plugins/` folder would be
misleading. See [OSS.md §9](./OSS.md#9-where-integration-code-should-live).

## Contents

- **[OSS.md](./OSS.md)** — the master assessment: what Fleet's open-source (MIT) edition can/can't
  do, the licensing/legal requirements to fork and sell it, the hosted-cloud dependencies to
  replace, our **decisions of record**, and designs for the target integrations (winget/Chocolatey/
  Ninite Pro, Action1, Bitdefender GravityZone, Huntress, Android UEM, Mosyle MDM, future IPaaS).
- **[PLUGIN-API-RFC.md](./PLUGIN-API-RFC.md)** — **the current plugin design of record**: a small,
  versioned Plugin API + an in-core adapter (version firewall) so compiled plugins survive minor Fleet
  upgrades; WASM primary / gRPC escape hatch; the upstream "host integration status" PR slice.
- **[PLUGINS.md](./PLUGINS.md)** — the earlier extension-point analysis (in-process provider registry,
  the capability taxonomy, the seam map). Superseded on the *distributable/cross-version* question by
  PLUGIN-API-RFC.md; still current for first-party in-process providers and the seam inventory.
- **[clean-room-protocol.md](./clean-room-protocol.md)** — the wall for legally reimplementing `ee/`
  features: implementers must never read `ee/` source. Enforced for AI agents too.
- **[clean-room-log.md](./clean-room-log.md)** — running record of describe-side vs. build-side work.
- **[MVP.md](./MVP.md)** — the minimal shippable product: the coverage-matrix device list (our NinjaRMM
  wedge), ScreenConnect, backups, and the build sequence.
- **[ZERO-TRUST.md](./ZERO-TRUST.md)** — deployment: device-plane vs admin-plane split, the exact
  public/gated route prefixes, Cloudflare Tunnel/Access/WAF config, and the WARP model.
- **[CONFIG-MGMT.md](./CONFIG-MGMT.md)** — "DevOps for endpoints": Fleet-native convergence loop + DSC v3 /
  masterless Salt as orbit-invoked executors (no Salt master), with WARP per-site config as the example.
- **[COLLECTORS.md](./COLLECTORS.md)** — collector-ingestion scaling (deferred until ~50k+ endpoints):
  push-first + Redis GCRA rate limiter as the throughput governor.
- **[setup/](./setup/)** — per-vendor operator setup guides (Mosyle, Action1, Bitdefender, Huntress,
  ScreenConnect, Veeam/iDrive360 backups).

## Planned additions

- `specs/` — functional specs for premium rebuilds (Teams, software deployment, host lock/wipe, …),
  authored per the clean-room protocol as the input implementers build from.
- `adr/` — architecture decision records.
- `runbooks/` — operational docs for the self-hosted infra we stand up (TUF update repo, CVE feed
  pipeline, GCP Android project, APNs certs).
