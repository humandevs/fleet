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
  replace, and rough designs for our target integrations (winget/Chocolatey/Ninite Pro, Action1,
  Bitdefender GravityZone, Huntress, Android UEM, Mosyle MDM, and the future IPaaS).

## Planned additions

- `adr/` — architecture decision records (e.g. tiering model, Apple-MDM strategy, multi-tenancy).
- `clean-room-log.md` — record of who read `ee/` (proprietary) vs. who implemented rebuilt features,
  as legal defense for the clean-room process.
- `runbooks/` — operational docs for the self-hosted infra we stand up (TUF update repo, CVE feed
  pipeline, GCP Android project, APNs certs).
