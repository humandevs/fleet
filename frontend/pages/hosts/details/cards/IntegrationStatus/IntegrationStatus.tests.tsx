import React from "react";
import { screen, render } from "@testing-library/react";

import { IHostIntegrationStatus } from "interfaces/integration_status";

import IntegrationStatus from ".";

const cell = (
  overrides: Partial<IHostIntegrationStatus>
): IHostIntegrationStatus => ({
  host_id: 1,
  source: "test",
  category: "av",
  state: "protected",
  detail: "",
  updated_at: "2026-07-04T00:00:00Z",
  stale: false,
  ...overrides,
});

describe("IntegrationStatus card", () => {
  it("renders a labeled cell per reported category", () => {
    render(
      <IntegrationStatus
        isLoading={false}
        data={[
          cell({ category: "av", state: "protected" }),
          cell({ category: "patching", source: "action1", state: "at_risk" }),
        ]}
      />
    );

    expect(screen.getByText("Coverage")).toBeInTheDocument();
    expect(screen.getByText("Managed AV")).toBeInTheDocument();
    expect(screen.getByText("Protected")).toBeInTheDocument();
    expect(screen.getByText("Patching")).toBeInTheDocument();
    expect(screen.getByText("At risk")).toBeInTheDocument();
  });

  it("shows the worst state when multiple sources report the same category", () => {
    render(
      <IntegrationStatus
        isLoading={false}
        data={[
          cell({ category: "av", source: "a", state: "protected" }),
          cell({ category: "av", source: "b", state: "at_risk" }),
        ]}
      />
    );

    // at_risk outranks protected → only "At risk" is shown for AV.
    expect(screen.getByText("At risk")).toBeInTheDocument();
    expect(screen.queryByText("Protected")).toBeNull();
  });

  it("renders an empty state when no cells are reported", () => {
    render(<IntegrationStatus isLoading={false} data={[]} />);
    expect(
      screen.getByText(/No coverage integrations are reporting/i)
    ).toBeInTheDocument();
  });

  it("does not render the empty state while loading", () => {
    render(<IntegrationStatus isLoading data={undefined} />);
    expect(
      screen.queryByText(/No coverage integrations are reporting/i)
    ).toBeNull();
  });
});
