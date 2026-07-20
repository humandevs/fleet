import React from "react";
import { screen } from "@testing-library/react";

import { PolicyResponse } from "utilities/constants";
import createMockPolicy from "__mocks__/policyMock";
import { createCustomRenderer } from "test/test-utils";

import HostsFilterBlock from "./HostsFilterBlock";

const COVERAGE_PILL_TEXT = "Coverage: problem devices";

type HostsFilterBlockProps = React.ComponentProps<typeof HostsFilterBlock>;

/**
 * Builds a minimal-but-valid props object for HostsFilterBlock. The component
 * destructures a large `params` object and many callbacks; everything unused by
 * a given test is passed as null/empty/jest.fn(). Callers override only the
 * params relevant to the filter under test.
 */
const createProps = (
  overrides: {
    params?: Partial<HostsFilterBlockProps["params"]>;
  } & Partial<Omit<HostsFilterBlockProps, "params">> = {}
): HostsFilterBlockProps => {
  const { params: paramOverrides, ...topLevelOverrides } = overrides;

  return {
    params: {
      munkiIssueDetails: null,
      policyResponse: PolicyResponse.PASSING,
      softwareDetails: null,
      mdmSolutionDetails: null,
      scriptBatchRanAt: null,
      scriptBatchScriptName: null,
      depProfileError: "",
      ...paramOverrides,
    },
    handleClearRouteParam: jest.fn(),
    handleClearFilter: jest.fn(),
    onChangePoliciesFilter: jest.fn(),
    onChangeOsSettingsFilter: jest.fn(),
    onChangeDiskEncryptionStatusFilter: jest.fn(),
    onChangeBootstrapPackageStatusFilter: jest.fn(),
    onChangeMacSettingsFilter: jest.fn(),
    onChangeSoftwareInstallStatusFilter: jest.fn(),
    onChangeConfigProfileStatusFilter: jest.fn(),
    onChangeScriptBatchStatusFilter: jest.fn(),
    onClickEditLabel: jest.fn(),
    onClickDeleteLabel: jest.fn(),
    ...topLevelOverrides,
  };
};

const render = createCustomRenderer({ context: { app: {} } });

describe("HostsFilterBlock - coverage problems filter", () => {
  it("renders the coverage problems pill when params.coverageProblems is true", () => {
    render(
      <HostsFilterBlock
        {...createProps({ params: { coverageProblems: true } })}
      />
    );

    expect(screen.getByText(COVERAGE_PILL_TEXT)).toBeInTheDocument();
  });

  it("clears the coverage_problems param when the pill's clear control is clicked", async () => {
    const handleClearFilter = jest.fn();

    const { user } = render(
      <HostsFilterBlock
        {...createProps({
          params: { coverageProblems: true },
          handleClearFilter,
        })}
      />
    );

    // The FilterPill's clear button carries the pill label as its `title`.
    await user.click(screen.getByTitle(COVERAGE_PILL_TEXT));

    expect(handleClearFilter).toHaveBeenCalledTimes(1);
    expect(handleClearFilter).toHaveBeenCalledWith(["coverage_problems"]);
  });

  it("prefers the policy pill over the coverage pill when a policy filter is also active", () => {
    const policy = createMockPolicy({ name: "Test policy" });

    render(
      <HostsFilterBlock
        {...createProps({
          params: {
            coverageProblems: true,
            policyId: policy.id,
            policy,
          },
        })}
      />
    );

    // policyId precedes coverageProblems in the render switch, so the policy
    // pill wins and the coverage pill is not rendered.
    expect(screen.getByText("Test policy")).toBeInTheDocument();
    expect(screen.queryByText(COVERAGE_PILL_TEXT)).toBeNull();
  });
});
