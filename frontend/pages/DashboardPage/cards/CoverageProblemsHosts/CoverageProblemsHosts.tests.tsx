import React from "react";
import { screen, render } from "@testing-library/react";

import CoverageProblemsHosts from ".";

// HostCountCard renders a react-router <Link> (via <Card path={...}>), which
// requires a <Router> to render. Mock it to a plain node that surfaces the
// `path` prop so we can assert on the manage-hosts URL the card builds.
jest.mock("../HostCountCard", () => ({
  __esModule: true,
  default: ({ path }: { path: string }) => (
    <div data-testid="host-count-card" data-path={path}>
      {path}
    </div>
  ),
}));

const getPath = () =>
  screen.getByTestId("host-count-card").getAttribute("data-path") ?? "";

describe("CoverageProblemsHosts card", () => {
  it("builds a manage-hosts link scoped to the current fleet", () => {
    render(
      <CoverageProblemsHosts coverageProblemsCount={7} currentTeamId={4} />
    );

    const path = getPath();
    // order-insensitive: both filters must be present
    expect(path).toContain("/hosts/manage");
    expect(path).toContain("coverage_problems=true");
    expect(path).toContain("fleet_id=4");
  });

  it("preserves fleet_id=0 (No team) instead of dropping the zero", () => {
    render(
      <CoverageProblemsHosts coverageProblemsCount={3} currentTeamId={0} />
    );

    const path = getPath();
    expect(path).toContain("coverage_problems=true");
    expect(path).toContain("fleet_id=0");
  });

  it("omits fleet_id entirely when no team is selected (All teams)", () => {
    render(
      <CoverageProblemsHosts
        coverageProblemsCount={5}
        currentTeamId={undefined}
      />
    );

    const path = getPath();
    expect(path).toContain("coverage_problems=true");
    expect(path).not.toContain("fleet_id");
  });

  it("uses the platform label route and still carries the coverage filter", () => {
    render(
      <CoverageProblemsHosts
        coverageProblemsCount={9}
        selectedPlatformLabelId={12}
        currentTeamId={4}
      />
    );

    const path = getPath();
    expect(path).toContain("/hosts/manage/labels/12");
    expect(path).toContain("coverage_problems=true");
  });
});
