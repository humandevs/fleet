# MVP.md — the minimal shippable product

> What we ship first, and exactly how the flagship feature (the **coverage-matrix device list**) is built.
> Builds on [`OSS.md`](./OSS.md) (the clean fork), [`PLUGINS.md`](./PLUGINS.md) (the provider seam), and
> [`ZERO-TRUST.md`](./ZERO-TRUST.md) (deployment). Internal dogfood first.

## The MVP thesis

NinjaRMM and friends **can't show, at a glance in the device list, whether each machine is actually
covered** — AV, MDR, remote access, backups, disk encryption. That per-device **coverage matrix** is our
wedge. The MVP = **clean fork + coverage-matrix hosts list + ScreenConnect + Zero-Trust deployment**,
scoped to what we already have data for plus the fastest new collectors.

## MVP scope

| In | Out (later) |
|---|---|
| Phase-0 clean fork (delete `ee/`, config-flag license → premium, rebrand, sever `ee/` imports) — [OSS.md §2.3, §10](./OSS.md#10-recommended-phasing--roadmap) | Full multi-tenancy rebuild (Teams XL), advanced native MDM, SCIM, conditional access |
| **Coverage-matrix device list** (BitLocker, AV, Remote Access, Backups, MDR, WARP) | IPaaS onboarding/offboarding |
| **ScreenConnect** deploy + online status + Connect link | Action1 patch control-plane UI (status ingest can come early) |
| **Backups** status (Veeam VSPC / iDrive360) | Native third-party patch scanner |
| **Zero-Trust** device/admin-plane split (Cloudflare) | Out-of-process gRPC plugins |
| Android (promote direct-Google client) + Mosyle ingest (already stable for Apple) | ChromeOS |

Everything rides **one pattern** — the `HostStatusProvider` + `host_integration_status` side table from
[PLUGINS.md §3.1](./PLUGINS.md#31-hoststatusprovider--coverage-matrix-backbone) — so each new signal is
"add a provider," not "re-plumb the host list."

## The coverage matrix — data reality per signal

Verified against the hosts-list pipeline (below). **Frontend columns are cheap; the backend collectors are
the real work.** Honest status of each of the six columns:

| Column | Data today | Effort | How |
|---|---|---|---|
| **BitLocker / disk encryption** | ✅ **Ingested** (`host_disks.bitlocker_protection_status`, `host_disk_encryption_keys`) but **not in the list SELECT** (only `GET /host/{id}`) | **S** | Add `hd.bitlocker_protection_status` to the `ListHosts` SELECT — `host_disks hd` is *already* JOINed. Lowest-effort win. |
| **AV (antivirus)** | ⚠️ **Absent in server.** Fleet only has an AV *policy* YAML; no per-host AV column. osquery *can* read Windows Security Center | **M** | New osquery detail query `windows_security_center`/`windows_security_products` (Windows) + ingest → `host_integration_status.av`. macOS/Linux need a policy/heuristic fallback (shows "not supported" otherwise). |
| **Remote Access (ScreenConnect)** | ❌ Absent | **M** | ScreenConnect poller → `host_integration_status.remote_access`. [setup/screenconnect.md](./setup/screenconnect.md) |
| **Backups (Veeam / iDrive360)** | ❌ Absent | **M–L** | Central API poller (VSPC OAuth / iDrive360 MSP key) → shared `Backups` detail; Veeam Event-Log osquery fallback. [setup/backups.md](./setup/backups.md) |
| **MDR** | ❌ Absent | **M** | Reuse the AV/EDR ingestion (Bitdefender GravityZone / Huntress) from [OSS.md §8.2](./OSS.md#82-avedr-dashboards-bitdefender-gravityzone--huntress) → `host_integration_status.mdr`. |
| **WARP** (Cloudflare) | ❌ Absent | **M** | osquery `services`/`processes`/`plist` (installed/running/org) + a `warp-cli status` script (enrolled/connected). [CONFIG-MGMT.md](./CONFIG-MGMT.md#warp-status-for-the-coverage-matrix-a-hoststatusprovider-signal) |

> **The load-bearing caveat:** MDR / Remote Access / Backups / WARP-enrollment are **not osquery-native** —
> "real" status (last backup time, MDR health, connected) needs a **central API poller** (Veeam/iDrive360/
> Bitdefender/Huntress/ScreenConnect) or an **authenticated orbit push endpoint**, not a detail query.
> Inferring mere "software installed" from `host_software` is weaker and can mislead. Budget for the
> collectors, not the columns.

## How the hosts list is built (verified pipeline)

```
GET /api/_version_/fleet/hosts
  → transport.go:122 hostListOptionsFromRequest        (opt-in flags; device_mapping at :362-368 = the template)
  → service/hosts.go:409 ListHosts → :449 StreamHosts   (authz ActionList; TeamFilter :458; hydration loop :478-528)
  → datastore/mysql/hosts.go:1087 ListHosts             (base SELECT + conditional column at :1144-1148; hostMDMSelect :1142)
  → hosts.go:1283 applyHostFilters                      (conditional LEFT JOINs; deviceMappingJoin :1289; FROM :1450-1499; team scope :1503)
  → []*fleet.Host (struct hosts.go:339)
Frontend: services/entities/hosts.ts:466 loadHosts (device_mapping=true) 
  → ManageHostsPage.tsx (useTeamIdParam → team/site scoping)
  → HostTableConfig.tsx:95 allHostTableHeaders → :726 generateAvailableTableHeaders (tier/role gate) → :768 visible
```

**"Extra" columns follow one pattern:** an opt-in boolean on `HostListOptions` gates **both** a SELECT
fragment **and** a LEFT JOIN (`device_mapping` is the exact template). **Team/site filtering needs no new
work** — `useTeamIdParam`/`teamIdForApi` already scopes the list, which is why the matrix lands naturally in
the per-client/site device view.

### Build steps (end-to-end, ~one vertical slice)
1. **Migration** — `host_integration_status` side table keyed by `host_id` (`av`, `mdr`, `remote_access`,
   `backup`, `warp` enums + `*_updated_at`); mirror `host_munki_info`. Read **BitLocker from the existing
   `host_disks`** — don't duplicate. **[S]** (`make migration`, `/new-migration`, add `_test.go`)
2. **Datastore** — `SetOrUpdateHostIntegrationStatus` + batch getter (copy `SetOrUpdateMunkiInfo`
   `datastore.go:1234`). **Run `go test ./server/service/`** after (mock regen). **[M]**
3. **List opt-in** — `PopulateIntegrationStatus` on `HostListOptions` (`hosts.go:183`, template
   `DeviceMapping`), parse in `transport.go:122`, conditional SELECT + JOIN in `datastore/mysql/hosts.go`,
   fields on `fleet.Host`. **Gate behind the opt-in flag** (a hot-path JOIN across thousands of hosts) and
   index `host_integration_status.host_id`. **[M]**
4. **BitLocker column** — add to the list SELECT (reuses existing `hd` join). **[S]**
5. **Collectors** — AV osquery query **[M]**; ScreenConnect/Backups/MDR/WARP providers **[M–L each]** as
   `HostStatusProvider`s (PLUGINS.md).
6. **Frontend** — icon-cell column group in `HostTableConfig.tsx:95` (model on `HostMdmStatusCell`/
   `IssueCell`/`StatusIndicator`), `IHost` fields (`interfaces/host.ts`), opt-in param in `loadHosts`,
   `defaultHiddenColumns` entry. **[M]** + a small 4-file status-icon cell component **[S]**.

**Files to touch:** `server/fleet/hosts.go`, `server/service/transport.go`, `server/datastore/mysql/hosts.go`,
a new migration, `server/fleet/datastore.go`, `server/service/osquery_utils/queries.go` (AV),
`frontend/interfaces/host.ts`, `frontend/pages/hosts/ManageHostsPage/HostTableConfig.tsx`,
`frontend/services/entities/hosts.ts`, `ManageHostsPage.tsx`.

## ScreenConnect MVP
Deploy (orbit URL-baked installer, `&c=` client/site handoff, capture SessionID) + online status
(`GuestConnectedCount`) + Connect deep link. macOS needs a pre-staged PPPC profile. Full detail:
[setup/screenconnect.md](./setup/screenconnect.md).

## Recommended MVP sequence
1. **Phase 0 clean fork** (OSS.md) — a rebranded, single-tier binary that builds without `ee/`.
2. **Coverage matrix vertical slice** with **BitLocker** (proves the whole path end-to-end, S/M effort, real data day one).
3. **Add collectors incrementally:** WARP + AV (osquery, cheap) → ScreenConnect → Backups → MDR (Bitdefender/Huntress).
4. **Zero-Trust deployment** (ZERO-TRUST.md) around the whole thing.
5. **Android promote + Mosyle ingest** for cross-platform device coverage.

Each collector is an independent `HostStatusProvider` — parallelizable, and each lights up one more matrix
column without touching the others.
