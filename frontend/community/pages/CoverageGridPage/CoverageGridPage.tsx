import React, { useCallback, useContext, useState } from "react";
import { useQuery } from "react-query";
import { InjectedRouter } from "react-router/lib/Router";
import { isEmpty, isEqual } from "lodash";

import hostsAPI, {
  ILoadHostsResponse,
  ISortOption,
} from "services/entities/hosts";
import PATHS from "router/paths";
import { AppContext } from "context/app";
import useTeamIdParam from "hooks/useTeamIdParam";

import { IHost } from "interfaces/host";

import { getNextLocationPath } from "utilities/helpers";
import { getPathWithQueryParams } from "utilities/url";
import {
  DEFAULT_PAGE_SIZE,
  DEFAULT_PAGE_INDEX,
  DEFAULT_SORT_DIRECTION,
} from "pages/hosts/ManageHostsPage/HostsPageConfig";

import MainContent from "components/MainContent";
import TableContainer from "components/TableContainer";
import { ITableQueryData } from "components/TableContainer/TableContainer";
import EmptyState from "components/EmptyState";
import FleetsDropdown from "components/FleetsDropdown";

import { generateCoverageGridColumns } from "./CoverageGridTableConfig";

const baseClass = "coverage-grid-page";

// Coverage grid defaults to the host name ascending. "hostname" matches the sortable Host column id.
const DEFAULT_SORT_HEADER = "hostname";

interface ICoverageGridPageProps {
  router: InjectedRouter;
  location: {
    pathname: string;
    search: string;
    hash?: string;
    query: {
      page?: string;
      query?: string;
      order_key?: string;
      order_direction?: string;
      fleet_id?: string;
      team_id?: string;
    };
  };
}

/** Renders the standard skeleton empty state for the coverage grid. */
const EmptyCoverageGrid = () => (
  <EmptyState
    header="No hosts"
    info="No hosts match the current filters. Try broadening your search or switching fleets."
  />
);

const CoverageGridPage = ({
  router,
  location,
}: ICoverageGridPageProps): JSX.Element => {
  const { config, isPremiumTier, isOnGlobalTeam } = useContext(AppContext);
  const isPrimoMode = config?.partnerships?.enable_primo;

  const {
    currentTeamId,
    teamIdForApi,
    isRouteOk,
    userTeams,
    handleTeamChange,
  } = useTeamIdParam({
    location,
    router,
    includeAllTeams: true,
    includeNoTeam: true,
  });

  // URL-driven table state.
  const queryParams = location.query;
  const page = queryParams?.page
    ? parseInt(queryParams.page, 10)
    : DEFAULT_PAGE_INDEX;
  const searchQuery = queryParams?.query ?? "";
  const sortHeader = queryParams?.order_key ?? DEFAULT_SORT_HEADER;
  const sortDirection = queryParams?.order_direction ?? DEFAULT_SORT_DIRECTION;
  const sortBy: ISortOption[] = [{ key: sortHeader, direction: sortDirection }];

  const [tableQueryData, setTableQueryData] = useState<ITableQueryData>();

  const { data, isFetching: isLoadingHosts } = useQuery<
    ILoadHostsResponse,
    Error
  >(
    // Query key must list every param the queryFn forwards to the API (cache-bleed rule).
    ["coverageGridHosts", teamIdForApi, sortBy, page, searchQuery],
    () =>
      hostsAPI.loadHosts({
        teamId: teamIdForApi,
        sortBy,
        page,
        perPage: DEFAULT_PAGE_SIZE,
        globalFilter: searchQuery,
        populateIntegrationStatus: true,
      }),
    {
      enabled: isRouteOk,
      keepPreviousData: true,
      staleTime: 10000,
    }
  );

  // Called once on initial render and every time the table query changes.
  const onQueryChange = useCallback(
    (newTableQuery: ITableQueryData) => {
      if (!isRouteOk || isEqual(newTableQuery, tableQueryData)) {
        return;
      }
      setTableQueryData({ ...newTableQuery });

      const {
        pageIndex,
        searchQuery: newSearchQuery,
        sortHeader: newSortHeader,
        sortDirection: newSortDirection,
      } = newTableQuery;

      const newQueryParams: Record<string, string | number | undefined> = {};
      if (!isEmpty(newSearchQuery)) {
        newQueryParams.query = newSearchQuery;
      }
      newQueryParams.page = pageIndex;
      newQueryParams.order_key = newSortHeader || DEFAULT_SORT_HEADER;
      newQueryParams.order_direction =
        newSortDirection || DEFAULT_SORT_DIRECTION;
      // Preserve fleet context. undefined (All teams) is filtered out of the query string.
      newQueryParams.fleet_id = teamIdForApi;

      router.replace(
        getNextLocationPath({
          pathPrefix: PATHS.COVERAGE_GRID,
          queryParams: newQueryParams,
        })
      );
    },
    [isRouteOk, tableQueryData, teamIdForApi, router]
  );

  const handleRowClick = (row: IHost) => {
    router.push(
      getPathWithQueryParams(PATHS.HOST_DETAILS(row.id), {
        fleet_id: teamIdForApi,
      })
    );
  };

  const renderHeaderContent = () => {
    if (isPremiumTier && !isPrimoMode && userTeams) {
      if (userTeams.length > 1 || isOnGlobalTeam) {
        return (
          <FleetsDropdown
            currentUserFleets={userTeams || []}
            selectedFleetId={currentTeamId}
            onChange={handleTeamChange}
            includeUnassigned
          />
        );
      }
      if (!isOnGlobalTeam && userTeams.length === 1) {
        return <h1>{userTeams[0].name}</h1>;
      }
    }
    return <h1>Coverage</h1>;
  };

  return (
    <MainContent className={baseClass}>
      <div className={`${baseClass}__wrapper`}>
        <div className={`${baseClass}__header`}>
          <div className={`${baseClass}__title`}>{renderHeaderContent()}</div>
        </div>
        <p className={`${baseClass}__subtitle`}>
          Per-host coverage across your fleet&apos;s security integrations.
        </p>
        <TableContainer
          columnConfigs={generateCoverageGridColumns(teamIdForApi)}
          data={data?.hosts || []}
          isLoading={isLoadingHosts}
          manualSortBy
          defaultSortHeader={DEFAULT_SORT_HEADER}
          defaultSortDirection={DEFAULT_SORT_DIRECTION}
          defaultSearchQuery={searchQuery}
          onQueryChange={onQueryChange}
          pageIndex={page}
          pageSize={DEFAULT_PAGE_SIZE}
          disableNextPage={(data?.hosts?.length || 0) < DEFAULT_PAGE_SIZE}
          searchable
          showMarkAllPages={false}
          isAllPagesSelected={false}
          emptyComponent={EmptyCoverageGrid}
          onClickRow={handleRowClick}
          disableMultiRowSelect
        />
      </div>
    </MainContent>
  );
};

export default CoverageGridPage;
