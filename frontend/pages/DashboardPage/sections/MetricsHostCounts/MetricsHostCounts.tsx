import React from "react";

import { LOW_DISK_SPACE_GB } from "pages/DashboardPage/helpers";

import { PlatformValueOptions } from "utilities/constants";
import CoverageProblemsHosts from "../../cards/CoverageProblemsHosts";
import LowDiskSpaceHosts from "../../cards/LowDiskSpaceHosts";
import MissingHosts from "../../cards/MissingHosts";
import TotalHosts from "../../cards/TotalHosts";
import ABMIssueHosts from "../../cards/ABMIssueHosts";

const baseClass = "metrics-host-counts";

interface IPlatformHostCountsProps {
  currentTeamId: number | undefined;
  selectedPlatform?: PlatformValueOptions;
  totalHostCount?: number;
  isPremiumTier?: boolean;
  missingCount: number;
  lowDiskSpaceCount: number;
  abmIssueCount: number;
  /** undefined hides the coverage tile (no community providers reporting). */
  coverageProblemsCount?: number;
  selectedPlatformLabelId?: number;
}

const MetricsHostCounts = ({
  currentTeamId,
  selectedPlatform,
  totalHostCount,
  isPremiumTier,
  missingCount,
  lowDiskSpaceCount,
  abmIssueCount,
  coverageProblemsCount,
  selectedPlatformLabelId,
}: IPlatformHostCountsProps): JSX.Element => {
  const TotalHostsCard = (
    <TotalHosts
      totalCount={totalHostCount}
      selectedPlatformLabelId={selectedPlatformLabelId}
      currentTeamId={currentTeamId}
    />
  );

  const MissingHostsCard = (
    <MissingHosts
      missingCount={missingCount}
      selectedPlatformLabelId={selectedPlatformLabelId}
      currentTeamId={currentTeamId}
    />
  );

  const LowDiskSpaceHostsCard = (
    <LowDiskSpaceHosts
      lowDiskSpaceGb={LOW_DISK_SPACE_GB}
      lowDiskSpaceCount={lowDiskSpaceCount}
      selectedPlatformLabelId={selectedPlatformLabelId}
      currentTeamId={currentTeamId}
      notSupported={selectedPlatform === "chrome"}
    />
  );

  // Renders only when the community coverage collector is reporting (count is undefined
  // otherwise); MIT/free feature, so not premium-gated. The count is not platform-filtered, so
  // the tile only shows on the "all platforms" view.
  const CoverageProblemsHostsCard =
    coverageProblemsCount !== undefined && selectedPlatform === "all" ? (
      <CoverageProblemsHosts
        coverageProblemsCount={coverageProblemsCount}
        selectedPlatformLabelId={selectedPlatformLabelId}
        currentTeamId={currentTeamId}
      />
    ) : null;

  // Does not render if abmIssueCount is 0 or undefined (e.g. on non-Apple platforms views)
  // Currently all undefined is defaulted to 0 upstream
  const ABMIssueHostsCard = abmIssueCount ? (
    <ABMIssueHosts
      abmIssueCount={abmIssueCount}
      selectedPlatformLabelId={selectedPlatformLabelId}
      currentTeamId={currentTeamId}
    />
  ) : null;

  const showMissingAndLowDiskHosts =
    selectedPlatform !== "ios" &&
    selectedPlatform !== "ipados" &&
    selectedPlatform !== "android";

  return (
    <div className={baseClass}>
      {selectedPlatform === "all" && TotalHostsCard}
      {CoverageProblemsHostsCard}
      {showMissingAndLowDiskHosts && MissingHostsCard}
      {/* Low disk space is Premium-only: `low_disk_space_count` is null for
          non-Premium callers and the linked filter is Premium-gated. */}
      {isPremiumTier && showMissingAndLowDiskHosts && LowDiskSpaceHostsCard}
      {/* ABM issue count is only populated on Premium (see DashboardPage). */}
      {ABMIssueHostsCard}
    </div>
  );
};

export default MetricsHostCounts;
