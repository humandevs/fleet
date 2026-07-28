import React from "react";
import PATHS from "router/paths";

import { getPathWithQueryParams } from "utilities/url";
import { HOSTS_QUERY_PARAMS } from "services/entities/hosts";

import HostCountCard from "../HostCountCard";

const baseClass = "hosts-coverage-problems";

interface ICoverageProblemsHostsProps {
  coverageProblemsCount: number;
  selectedPlatformLabelId?: number;
  currentTeamId?: number;
}

/** Coverage "problem devices" tile: hosts with no coverage data at all, or with any coverage
 * category (AV, MDR, remote access, backups, disk encryption, patching) that isn't protected.
 * Stale data counts as unknown, so a host whose provider stopped reporting shows up here. */
const CoverageProblemsHosts = ({
  coverageProblemsCount,
  selectedPlatformLabelId,
  currentTeamId,
}: ICoverageProblemsHostsProps): JSX.Element => {
  // build the manage hosts URL filtered to coverage problem devices
  const queryParams = {
    [HOSTS_QUERY_PARAMS.COVERAGE_PROBLEMS]: "true",
    fleet_id: currentTeamId,
  };

  const endpoint = selectedPlatformLabelId
    ? PATHS.MANAGE_HOSTS_LABEL(selectedPlatformLabelId)
    : PATHS.MANAGE_HOSTS;
  const path = getPathWithQueryParams(endpoint, queryParams);

  return (
    <HostCountCard
      iconName="warning"
      count={coverageProblemsCount}
      title="Problem devices"
      tooltip="Hosts with no coverage data, or with a coverage category (AV, MDR, remote access, backups, disk encryption, patching) that isn't protected. Stale data counts as unknown."
      path={path}
      className={baseClass}
      iconPosition="left"
    />
  );
};

export default CoverageProblemsHosts;
