import React from "react";
import { screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";

import { createCustomRenderer, baseUrl, createMockRouter } from "test/test-utils";
import mockServer from "test/mock-server";
import createMockConfig from "__mocks__/configMock";
import createMockUser from "__mocks__/userMock";
import { IIntegrationStatusSummaryItem } from "interfaces/integration_status";

import DashboardPage from "./DashboardPage";

// Free-tier context: with no fleet_id in the URL, useTeamIdParam resolves
// isRouteOk synchronously (see the isFreeTier branch), so the page mounts and
// fires its queries without needing the premium /fleets round-trip. teamIdForApi
// ends up undefined ("All teams"), which is all the coverage gating cares about.
const mockAppContext = {
  isGlobalAdmin: false,
  isGlobalMaintainer: false,
  isOnGlobalTeam: false,
  isPremiumTier: false,
  isFreeTier: true,
  currentUser: createMockUser({ global_role: "admin" }),
  config: createMockConfig(),
  availableTeams: [],
  currentTeam: undefined,
  setCurrentTeam: jest.fn(),
};

const createMockProps = () => ({
  router: createMockRouter(),
  location: {
    pathname: "/dashboard",
    search: "",
    hash: "",
    query: {},
  },
});

// Handlers -------------------------------------------------------------------

// The org name is rendered as the page <h1> on free tier, so a distinctive
// value gives a reliable "page has mounted and config loaded" anchor.
const getConfigHandler = () =>
  http.get(baseUrl("/config"), () =>
    HttpResponse.json(
      createMockConfig({
        org_info: {
          org_name: "Test Corp",
          org_logo_url: "",
          org_logo_url_light_background: "",
          contact_url: "",
        },
      })
    )
  );

const getHostSummaryHandler = (totalsHostsCount = 12) =>
  http.get(baseUrl("/host_summary"), () =>
    HttpResponse.json({
      all_linux_count: 0,
      totals_hosts_count: totalsHostsCount,
      platforms: [],
      online_count: totalsHostsCount,
      offline_count: 0,
      mia_count: 0,
      new_count: 0,
      builtin_labels: [],
    })
  );

// hosts_count > 0 keeps the MDM card visible; the deep destructure in the
// page's mdm onSuccess needs the full enrollment-status shape or it throws.
const getMdmSummaryHandler = () =>
  http.get(baseUrl("/hosts/summary/mdm"), () =>
    HttpResponse.json({
      counts_updated_at: "2026-07-04T00:00:00Z",
      mobile_device_management_enrollment_status: {
        enrolled_manual_hosts_count: 1,
        enrolled_automated_hosts_count: 0,
        enrolled_personal_hosts_count: 0,
        unenrolled_hosts_count: 0,
        pending_hosts_count: 0,
        hosts_count: 1,
      },
      mobile_device_management_solution: null,
    })
  );

const getSoftwareHandler = () =>
  http.get(baseUrl("/software"), () =>
    HttpResponse.json({ software: [], counts_updated_at: "" })
  );

const getChartsHandler = () =>
  http.get(baseUrl("/charts/:metric"), () =>
    HttpResponse.json({ data: [], total_hosts: 0 })
  );

const getIntegrationStatusSummaryHandler = (
  summary: IIntegrationStatusSummaryItem[] | null,
  onCall?: () => void
) =>
  http.get(baseUrl("/host_integration_status/summary"), () => {
    onCall?.();
    return HttpResponse.json({ integration_status_summary: summary });
  });

const getHostsCountHandler = (count: number, record: URL[]) =>
  http.get(baseUrl("/hosts/count"), ({ request }) => {
    record.push(new URL(request.url));
    return HttpResponse.json({ count });
  });

interface ISetupOptions {
  summary: IIntegrationStatusSummaryItem[] | null;
  hostsCount: number;
  countRequests: URL[];
  onSummaryCall?: () => void;
}

const setupHandlers = ({
  summary,
  hostsCount,
  countRequests,
  onSummaryCall,
}: ISetupOptions) => {
  mockServer.use(
    getConfigHandler(),
    getHostSummaryHandler(),
    getIntegrationStatusSummaryHandler(summary, onSummaryCall),
    getHostsCountHandler(hostsCount, countRequests),
    getMdmSummaryHandler(),
    getSoftwareHandler(),
    getChartsHandler()
  );
};

const coverageProblemsRequests = (requests: URL[]) =>
  requests.filter((u) => u.searchParams.get("coverage_problems") === "true");

describe("DashboardPage - coverage problems gating", () => {
  it("hides the coverage 'Problem devices' tile and never issues the count query when the summary reports no coverage data", async () => {
    const countRequests: URL[] = [];
    let summaryCalls = 0;
    setupHandlers({
      summary: null,
      hostsCount: 0,
      countRequests,
      onSummaryCall: () => {
        summaryCalls += 1;
      },
    });

    const render = createCustomRenderer({
      withBackendMock: true,
      context: { app: mockAppContext },
    });

    render(<DashboardPage {...(createMockProps() as any)} />);

    // Dashboard mounts and the config-derived header renders.
    expect(
      await screen.findByRole("heading", { name: "Test Corp" }, { timeout: 5000 })
    ).toBeInTheDocument();

    // The host-counts section renders its always-present "Total hosts" tile,
    // which only appears once the host summary query has resolved and committed.
    expect(
      await screen.findByText("Total hosts", undefined, { timeout: 5000 })
    ).toBeInTheDocument();

    // Make sure the coverage summary actually resolved (so hasCoverageData has
    // been evaluated as false) before asserting the gated behavior.
    await waitFor(() => expect(summaryCalls).toBeGreaterThan(0));

    // Gated off: no tile...
    expect(screen.queryByText("Problem devices")).not.toBeInTheDocument();
    // ...and the coverage-problems host count was never requested.
    expect(coverageProblemsRequests(countRequests)).toHaveLength(0);
  });

  it("fetches and shows the coverage 'Problem devices' tile with the returned count when the summary reports coverage data", async () => {
    const countRequests: URL[] = [];
    setupHandlers({
      summary: [
        { source: "bitdefender", category: "av", state: "protected", count: 3 },
      ],
      hostsCount: 7,
      countRequests,
    });

    const render = createCustomRenderer({
      withBackendMock: true,
      context: { app: mockAppContext },
    });

    render(<DashboardPage {...(createMockProps() as any)} />);

    // The tile only renders after the whole chain resolves: summary reports
    // coverage data -> hasCoverageData true -> count query enabled -> count
    // returned, so finding it confirms the gating opened.
    const tile = await screen.findByText("Problem devices", undefined, {
      timeout: 5000,
    });
    expect(tile).toBeInTheDocument();

    // The count (7) is rendered inside the same coverage card.
    const card = tile.closest(".host-count-card");
    expect(card).not.toBeNull();
    expect(within(card as HTMLElement).getByText("7")).toBeInTheDocument();

    // The count came from a /hosts/count request carrying coverage_problems=true.
    await waitFor(() =>
      expect(coverageProblemsRequests(countRequests).length).toBeGreaterThan(0)
    );
  });
});
