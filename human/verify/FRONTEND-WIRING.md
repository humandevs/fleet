# Frontend wiring: coverage matrix card + host-list coverage filter

Status of the frontend coverage work, and the exact remaining wiring to apply **on the VM** (where webpack
+ Puppeteer can verify it end-to-end).

> **Verified on the dev host:** `yarn install` is done, so `npx tsc --noEmit` passes clean for the whole
> frontend (card, filter control, service methods, interfaces, HostDetailsPage wiring all type-correct), and
> the card's jest test (`IntegrationStatus.tests.tsx`) passes (4/4: labeled cells, worst-state-per-category,
> empty state, loading gate). What still needs the VM is the *runtime render* (Puppeteer screenshot) and the
> ManageHostsPage filter wiring below.

## Done (built + type-checked + unit-tested locally)

**Coverage matrix card — fully wired.**
- `frontend/interfaces/integration_status.ts` — types.
- `frontend/pages/hosts/details/cards/IntegrationStatus/` — the card (mirrors the MacAdmins/Munki card:
  `Card` + `CardHeader` + a grid of `DataSet` → `StatusIndicatorWithIcon`, worst-state-per-category).
- `frontend/utilities/endpoints.ts` — `HOST_INTEGRATION_STATUS(id)`, `HOSTS_COVERAGE`.
- `frontend/services/entities/hosts.ts` — `getIntegrationStatus(hostID)`, `getHostsByCoverage(coverage)`,
  `HOSTS_QUERY_PARAMS.COVERAGE`.
- `frontend/pages/hosts/details/HostDetailsPage/HostDetailsPage.tsx` — `useQuery(["integrationStatus", …])`
  + `<IntegrationStatusCard>` rendered in the **Details** TabPanel (after Vitals). Shows only when a
  provider has reported cells. **Verify:** `npm run smoke` → `04-host-details-coverage.png`.

**Coverage filter — control + backend done; page wiring staged (below).**
- `frontend/pages/hosts/ManageHostsPage/components/CoverageFilter/` — the `DropdownWrapper` control +
  options ("Only problems", "Missing managed AV", …).
- Backend: `GET /api/latest/fleet/hosts/coverage?problems=…&missing=…&category=…&state=…` →
  `{count, host_ids}` (built + unit-tested: `TestCoverageFilterFromRequest`, `coverageFilterConds`).

## Remaining: wire `CoverageFilter` into ManageHostsPage (apply on the VM)

`ManageHostsPage.tsx` is a 2,179-line stateful, URL-driven page — do this with the server running so each
step is visually verifiable. Anchors from the scout report; confirm line numbers before editing.

1. **`frontend/services/entities/hosts.ts`** — `HOSTS_QUERY_PARAMS.COVERAGE` is added. In `loadHosts`
   (`queryParams` object, ~:503) add `[HOSTS_QUERY_PARAMS.COVERAGE]: coverage` alongside `status`
   (it's an independent overlay — bypass the `reconcileMutuallyExclusive*` helpers). Add `coverage?: string`
   to `ILoadHostsOptions` (~:108).
2. **`frontend/services/entities/host_count.ts`** — mirror the same two additions (`IHostCountLoadOptions`
   ~:32 and the `load()` `queryParams` ~:95). The count query MUST match the list query or the "N hosts"
   header desyncs.
3. **`ManageHostsPage.tsx`** parse param (~:347): `const coverage = queryParams?.coverage;`.
4. Add `coverage` to BOTH `useQuery` key objects — hosts (~:593) and count (~:644).
5. Add handler (copy `handleChangeOsSettingsFilter` ~:860):
   `const handleCoverageChange = (v) => router.replace(getNextLocationPath({ …routeParams,
   queryParams: { ...queryParams, [HOSTS_QUERY_PARAMS.COVERAGE]: v || undefined, page: 0 } }));`
6. Render the control in `renderCustomControls` (~:1778), next to the status `DropdownWrapper`:
   `<CoverageFilter value={coverage} onChange={handleCoverageChange} isDisabled={isTrulyEmpty} />`.
7. `HostsPageConfig.tsx` (~:3) — add `"coverage"` to `MANAGE_HOSTS_PAGE_FILTER_KEYS`.
8. *(optional)* removable `FilterPill` via `HostsFilterBlock.tsx` switch (~:701) → `handleClearFilter(["coverage"])`.

### The one backend decision this forces

Steps 1–2 send `?coverage=…` to the **existing `GET /hosts`** list endpoint. Two ways to honor it:

- **(a) Thread it into `ListHosts` (proper, idiomatic):** add `CoverageFilter` to `fleet.HostListOptions`,
  decode `coverage` in the host-list endpoint, and `AND` the already-unit-tested `coverageFilterConds`
  fragment into the `ListHosts` MySQL query (confirm the host alias — the fragment uses `h.id`). Additive,
  guarded by `IsZero()`. **Must be MySQL-round-trip tested** — this is exactly why it's a VM task, not a
  blind edit: a broken `ListHosts` query breaks the entire hosts page.
- **(b) Standalone endpoint (already built):** keep `GET /hosts/coverage` and have the filter show a
  matching count / drill list without touching `ListHosts`. Lower risk, lesser UX.

Recommendation: ship (b)'s control now for feedback; do (a) on the VM with a `MYSQL_TEST` round-trip before
relying on in-table filtering.
