import React from "react";
import { CellProps, Column } from "react-table";

import { IHost } from "interfaces/host";
import {
  IntegrationCoverageCategory,
  IntegrationCoverageState,
} from "interfaces/integration_status";
import {
  IHeaderProps,
  IStringCellProps,
  INumberCellProps,
} from "interfaces/datatable_config";

import PATHS from "router/paths";
import { getPathWithQueryParams } from "utilities/url";
import { humanHostMemory, secondsToHms } from "utilities/helpers";
import { DEFAULT_EMPTY_CELL_VALUE } from "utilities/constants";

import DiskSpaceIndicator from "pages/hosts/components/DiskSpaceIndicator";
import HeaderCell from "components/TableContainer/DataTable/HeaderCell/HeaderCell";
import LinkCell from "components/TableContainer/DataTable/LinkCell/LinkCell";
import TextCell from "components/TableContainer/DataTable/TextCell/TextCell";
import TooltipTruncatedTextCell from "components/TableContainer/DataTable/TooltipTruncatedTextCell";
import TooltipWrapper from "components/TooltipWrapper";
import NotSupported from "components/NotSupported";
import StatusIndicatorWithIcon from "components/StatusIndicatorWithIcon";
import { IndicatorStatus } from "components/StatusIndicatorWithIcon/StatusIndicatorWithIcon";
import { HumanTimeDiffWithFleetLaunchCutoff } from "components/HumanTimeDiffWithDateTip";

type ICoverageGridHeaderProps = IHeaderProps<IHost>;
type ICoverageGridStringCellProps = IStringCellProps<IHost>;
type ICoverageGridNumberCellProps = INumberCellProps<IHost>;
// Coverage / disk / cpu cells read only from row.original, so a generic cell type is sufficient.
type ICoverageGridCellProps = CellProps<IHost>;

// Lower = more severe. When multiple sources report the same category we show the worst cell so a
// gap is never hidden by a healthy one. Mirrors the host-details Coverage card.
const STATE_SEVERITY: Record<IntegrationCoverageState, number> = {
  at_risk: 0,
  not_installed: 1,
  unknown: 2,
  protected: 3,
};

const STATE_INDICATOR: Record<
  IntegrationCoverageState,
  { status: IndicatorStatus; label: string }
> = {
  protected: { status: "success", label: "Protected" },
  at_risk: { status: "actionRequired", label: "At risk" },
  not_installed: { status: "failure", label: "Not installed" },
  unknown: { status: "pending", label: "Unknown" },
};

/**
 * Reduces a host's integration_status cells to the single worst cell for the given category and
 * renders it. If the host has no cell for the category, an em-dash placeholder is rendered.
 */
const renderCoverageCell = (
  host: IHost,
  category: IntegrationCoverageCategory
): JSX.Element => {
  const cells = (host.integration_status ?? []).filter(
    (cell) => cell.category === category
  );

  if (cells.length === 0) {
    return <TextCell grey value={DEFAULT_EMPTY_CELL_VALUE} />;
  }

  const worst = cells.reduce((acc, cell) =>
    STATE_SEVERITY[cell.state] < STATE_SEVERITY[acc.state] ? cell : acc
  );
  const indicator = STATE_INDICATOR[worst.state];

  return (
    <StatusIndicatorWithIcon
      status={indicator.status}
      value={indicator.label}
      tooltip={{
        tooltipText: worst.detail
          ? `${worst.source}: ${worst.detail}`
          : worst.source,
        position: "top",
      }}
    />
  );
};

/**
 * Generates the per-host coverage grid columns (GFI / N-able RMM style, one row per host).
 * Only Host, Uptime, and Last reported are sortable — coverage/disk/cpu columns are not.
 */
const generateCoverageGridColumns = (teamId?: number): Column<IHost>[] => [
  // Host
  {
    Header: (cellProps: ICoverageGridHeaderProps) => (
      <HeaderCell value="Host" isSortedDesc={cellProps.column.isSortedDesc} />
    ),
    id: "hostname",
    accessor: (host) => host.display_name || host.hostname,
    Cell: (cellProps: ICoverageGridStringCellProps) => (
      <LinkCell
        value={cellProps.cell.value}
        path={getPathWithQueryParams(
          PATHS.HOST_DETAILS(cellProps.row.original.id),
          { fleet_id: teamId }
        )}
        tooltipTruncate
      />
    ),
  },
  // User
  {
    Header: () => <HeaderCell value="User" disableSortBy />,
    id: "user",
    disableSortBy: true,
    accessor: (host) =>
      host.device_mapping?.[0]?.email ||
      host.end_users?.[0]?.idp_username ||
      host.end_users?.[0]?.idp_full_name ||
      "",
    Cell: (cellProps: ICoverageGridStringCellProps) => (
      <TooltipTruncatedTextCell value={cellProps.cell.value} />
    ),
  },
  // Uptime (not sortable: "uptime" is not in the backend host order-key allowlist, so sorting on it 422s)
  {
    Header: () => <HeaderCell value="Uptime" disableSortBy />,
    id: "uptime",
    disableSortBy: true,
    accessor: "uptime",
    Cell: (cellProps: ICoverageGridNumberCellProps) => (
      <TextCell value={cellProps.cell.value} formatter={secondsToHms} />
    ),
  },
  // Last reported
  {
    Header: (cellProps: ICoverageGridHeaderProps) => (
      <HeaderCell
        value="Last reported"
        isSortedDesc={cellProps.column.isSortedDesc}
      />
    ),
    id: "detail_updated_at",
    accessor: "detail_updated_at",
    Cell: (cellProps: ICoverageGridStringCellProps) => (
      <TextCell
        value={{ timeString: cellProps.cell.value }}
        formatter={HumanTimeDiffWithFleetLaunchCutoff}
      />
    ),
  },
  // ScreenConnect (remote access)
  {
    Header: () => <HeaderCell value="ScreenConnect" disableSortBy />,
    id: "remote_access",
    disableSortBy: true,
    Cell: (cellProps: ICoverageGridCellProps) =>
      renderCoverageCell(cellProps.row.original, "remote_access"),
  },
  // AV
  {
    Header: () => <HeaderCell value="AV" disableSortBy />,
    id: "av",
    disableSortBy: true,
    Cell: (cellProps: ICoverageGridCellProps) =>
      renderCoverageCell(cellProps.row.original, "av"),
  },
  // Patch
  {
    Header: () => <HeaderCell value="Patch" disableSortBy />,
    id: "patching",
    disableSortBy: true,
    Cell: (cellProps: ICoverageGridCellProps) =>
      renderCoverageCell(cellProps.row.original, "patching"),
  },
  // Disk
  {
    Header: () => <HeaderCell value="Disk" disableSortBy />,
    id: "disk",
    disableSortBy: true,
    accessor: "gigs_disk_space_available",
    Cell: (cellProps: ICoverageGridCellProps) => {
      const {
        platform,
        percent_disk_space_available,
        gigs_disk_space_available,
        gigs_total_disk_space,
        gigs_all_disk_space,
      } = cellProps.row.original;
      if (platform === "chrome") {
        return NotSupported;
      }
      return (
        <DiskSpaceIndicator
          gigsDiskSpaceAvailable={gigs_disk_space_available}
          percentDiskSpaceAvailable={percent_disk_space_available}
          gigsTotalDiskSpace={gigs_total_disk_space}
          gigsAllDiskSpace={gigs_all_disk_space}
          platform={platform}
        />
      );
    },
  },
  // CPU / memory — greyed out: this metric is coarse (daily) and not live yet.
  {
    Header: () => (
      <HeaderCell
        value={
          <TooltipWrapper tipContent="Updated daily (coarse); moving to hourly.">
            <span className="coverage-grid-page__muted-header">
              CPU / memory
            </span>
          </TooltipWrapper>
        }
        disableSortBy
      />
    ),
    id: "cpu_memory",
    disableSortBy: true,
    accessor: "memory",
    Cell: (cellProps: ICoverageGridCellProps) => (
      <TextCell grey value={humanHostMemory(cellProps.row.original.memory)} />
    ),
  },
];

export default generateCoverageGridColumns;
export { generateCoverageGridColumns };
