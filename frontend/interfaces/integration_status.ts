// Per-host third-party coverage status (community-plugin coverage matrix). Mirrors
// server/fleet/host_integration_status.go. MIT/free feature — no premium license required.

export type IntegrationCoverageCategory =
  | "av"
  | "mdr"
  | "remote_access"
  | "backups"
  | "disk_encryption"
  | "patching";

export type IntegrationCoverageState =
  | "protected"
  | "at_risk"
  | "not_installed"
  | "unknown";

export interface IHostIntegrationStatus {
  host_id: number;
  source: string;
  category: IntegrationCoverageCategory;
  state: IntegrationCoverageState;
  detail: string;
  updated_at: string;
  // stale is set by the server when the cell is older than its category TTL; state is then "unknown".
  stale: boolean;
}

export interface IHostIntegrationStatusResponse {
  integration_status: IHostIntegrationStatus[];
}


/** One row of the fleet-wide coverage rollup: how many cells a provider reported for a
 * (source, category, state) combination. Cells past their freshness TTL are counted as "unknown"
 * by the server. */
export interface IIntegrationStatusSummaryItem {
  source: string;
  category: IntegrationCoverageCategory;
  state: IntegrationCoverageState;
  count: number;
}

export interface IHostIntegrationStatusSummaryResponse {
  integration_status_summary: IIntegrationStatusSummaryItem[] | null;
}
