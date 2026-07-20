import React from "react";
import { render, screen, within } from "@testing-library/react";

import { PlatformValueOptions } from "utilities/constants";

import MetricsHostCounts from ".";

// The coverage tile is rendered by the CoverageProblemsHosts card as a HostCountCard titled
// "Problem devices". The card's title text is the reliable discriminator for the tile's presence.
const COVERAGE_TILE_TITLE = "Problem devices";

interface IRenderOptions {
  selectedPlatform?: PlatformValueOptions;
  coverageProblemsCount?: number;
  isPremiumTier?: boolean;
}

const renderMetricsHostCounts = ({
  selectedPlatform,
  coverageProblemsCount,
  isPremiumTier,
}: IRenderOptions) =>
  render(
    <MetricsHostCounts
      currentTeamId={undefined}
      selectedPlatform={selectedPlatform}
      coverageProblemsCount={coverageProblemsCount}
      isPremiumTier={isPremiumTier}
      totalHostCount={undefined}
      missingCount={0}
      lowDiskSpaceCount={0}
      abmIssueCount={0}
    />
  );

describe("MetricsHostCounts - coverage tile", () => {
  it("does not render the coverage tile when coverageProblemsCount is undefined", () => {
    renderMetricsHostCounts({
      selectedPlatform: "all",
      coverageProblemsCount: undefined,
    });

    expect(screen.queryByText(COVERAGE_TILE_TITLE)).toBeNull();
  });

  it("renders the coverage tile showing 0 on free tier when the count is 0 (pins the `!== undefined` semantics and free-tier availability)", () => {
    renderMetricsHostCounts({
      selectedPlatform: "all",
      coverageProblemsCount: 0,
      isPremiumTier: false,
    });

    const title = screen.getByText(COVERAGE_TILE_TITLE);
    expect(title).toBeInTheDocument();

    // Scope the count lookup to the tile itself: the "all platforms" view also renders a Total
    // hosts card whose count is "0", so an unscoped getByText("0") would be ambiguous.
    const tile = title.closest(".host-count-card") as HTMLElement;
    expect(within(tile).getByText("0")).toBeInTheDocument();
  });

  it("does not render the coverage tile on a platform-filtered (non-all) view", () => {
    renderMetricsHostCounts({
      selectedPlatform: "windows",
      coverageProblemsCount: 5,
    });

    expect(screen.queryByText(COVERAGE_TILE_TITLE)).toBeNull();
  });

  it("renders the coverage tile showing the count on the all-platforms view", () => {
    renderMetricsHostCounts({
      selectedPlatform: "all",
      coverageProblemsCount: 5,
    });

    const title = screen.getByText(COVERAGE_TILE_TITLE);
    expect(title).toBeInTheDocument();

    const tile = title.closest(".host-count-card") as HTMLElement;
    expect(within(tile).getByText("5")).toBeInTheDocument();
  });
});
