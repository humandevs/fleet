/* eslint-disable  @typescript-eslint/explicit-module-boundary-types */
import sendRequest from "services";
import { buildQueryStringFromParams } from "utilities/url";
import communityEndpoints from "community/endpoints";

import {
  IHostIntegrationStatusResponse,
  IHostIntegrationStatusSummaryResponse,
} from "interfaces/integration_status";

interface IGetSummaryProps {
  teamId?: number;
}

export default {
  // getIntegrationStatus fetches the community-plugin coverage cells for a host (AV/MDR/patching/remote
  // access/backups/disk encryption). Feeds the host-details Coverage card.
  getIntegrationStatus: (
    hostID: number
  ): Promise<IHostIntegrationStatusResponse> => {
    return sendRequest(
      "GET",
      communityEndpoints.HOST_INTEGRATION_STATUS(hostID)
    );
  },

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
    const endpoint = communityEndpoints.HOST_INTEGRATION_STATUS_SUMMARY;
    const path = queryString ? `${endpoint}?${queryString}` : endpoint;

    return sendRequest("GET", path);
  },
};
