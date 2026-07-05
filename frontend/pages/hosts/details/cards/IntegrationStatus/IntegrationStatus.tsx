import React from "react";
import classnames from "classnames";

import {
  IHostIntegrationStatus,
  IntegrationCoverageCategory,
  IntegrationCoverageState,
} from "interfaces/integration_status";

import Card from "components/Card";
import CardHeader from "components/CardHeader";
import DataSet from "components/DataSet";
import Spinner from "components/Spinner";
import StatusIndicatorWithIcon from "components/StatusIndicatorWithIcon";
import { IndicatorStatus } from "components/StatusIndicatorWithIcon/StatusIndicatorWithIcon";

const baseClass = "integration-status-card";

const CATEGORY_LABELS: Record<IntegrationCoverageCategory, string> = {
  av: "Managed AV",
  mdr: "MDR / EDR",
  patching: "Patching",
  remote_access: "Remote access",
  backups: "Backups",
  disk_encryption: "Disk encryption",
};

// Fixed display order for the coverage columns.
const CATEGORY_ORDER: IntegrationCoverageCategory[] = [
  "av",
  "mdr",
  "patching",
  "remote_access",
  "backups",
  "disk_encryption",
];

// Lower = more severe. When multiple sources report the same category, we show the worst cell so a gap is
// never hidden by a healthy one.
const STATE_SEVERITY: Record<IntegrationCoverageState, number> = {
  at_risk: 0,
  not_installed: 1,
  unknown: 2,
  protected: 3,
};

const STATE_INDICATOR: Record<
  IntegrationCoverageState,
  { status: IndicatorStatus; label: string }
> = {
  protected: { status: "success", label: "Protected" },
  at_risk: { status: "actionRequired", label: "At risk" },
  not_installed: { status: "failure", label: "Not installed" },
  unknown: { status: "pending", label: "Unknown" },
};

interface IIntegrationStatusCardProps {
  isLoading: boolean;
  data?: IHostIntegrationStatus[];
  className?: string;
}

const IntegrationStatusCard = ({
  isLoading,
  data,
  className,
}: IIntegrationStatusCardProps): JSX.Element => {
  // Reduce to the most severe cell per category.
  const worstByCategory = new Map<
    IntegrationCoverageCategory,
    IHostIntegrationStatus
  >();
  (data ?? []).forEach((cell) => {
    const current = worstByCategory.get(cell.category);
    if (!current || STATE_SEVERITY[cell.state] < STATE_SEVERITY[current.state]) {
      worstByCategory.set(cell.category, cell);
    }
  });

  const cells = CATEGORY_ORDER.filter((c) => worstByCategory.has(c)).map((c) => {
    const cell = worstByCategory.get(c) as IHostIntegrationStatus;
    const indicator = STATE_INDICATOR[cell.state];
    return (
      <DataSet
        key={c}
        title={CATEGORY_LABELS[c]}
        value={
          <StatusIndicatorWithIcon
            status={indicator.status}
            value={indicator.label}
            tooltip={{
              tooltipText: cell.detail
                ? `${cell.source}: ${cell.detail}`
                : cell.source,
              position: "top",
            }}
          />
        }
      />
    );
  });

  const renderBody = () => {
    if (isLoading) {
      return <Spinner />;
    }
    if (cells.length === 0) {
      return (
        <p className={`${baseClass}__empty`}>
          No coverage integrations are reporting for this host yet.
        </p>
      );
    }
    return <div className={`${baseClass}__grid`}>{cells}</div>;
  };

  return (
    <Card
      className={classnames(baseClass, className)}
      borderRadiusSize="xxlarge"
      paddingSize="xlarge"
    >
      <CardHeader header="Coverage" />
      {renderBody()}
    </Card>
  );
};

export default IntegrationStatusCard;
