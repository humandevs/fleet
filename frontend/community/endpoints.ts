/**
 * Community (fork) API endpoints — kept out of utilities/endpoints.ts (a hot upstream file) so the
 * fork's rebase surface stays confined to community-owned modules.
 *
 * API_VERSION mirrors the (unexported) constant in utilities/endpoints.ts; both are "latest".
 */
const API_VERSION = "latest";

export const communityEndpoints = {
  HOST_INTEGRATION_STATUS: (id: number): string =>
    `/${API_VERSION}/fleet/hosts/${id}/integration_status`,
  HOST_INTEGRATION_STATUS_SUMMARY: `/${API_VERSION}/fleet/host_integration_status/summary`,
} as const;

export default communityEndpoints;
