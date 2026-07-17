/* eslint-disable  @typescript-eslint/explicit-module-boundary-types */
import sendRequest from "services";
import endpoints from "utilities/endpoints";
import { buildQueryStringFromParams } from "utilities/url";

import { IHostIntegrationStatusSummaryResponse } from "interfaces/integration_status";

interface IGetSummaryProps {
  teamId?: number;
}

export default {
  // getSummary fetches the fleet-wide (optionally fleet-scoped) coverage rollup grouped by
  // (source, category, state). Cells past their freshness TTL are counted as "unknown" by the
  // server, so these counts agree with the coverage host filters.
  getSummary: ({
    teamId,
  }: IGetSummaryProps): Promise<IHostIntegrationStatusSummaryResponse> => {
    const queryParams = {
      fleet_id: teamId,
    };

    const queryString = buildQueryStringFromParams(queryParams);
    const endpoint = endpoints.HOST_INTEGRATION_STATUS_SUMMARY;
    const path = queryString ? `${endpoint}?${queryString}` : endpoint;

    return sendRequest("GET", path);
  },
};
