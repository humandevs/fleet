import React from "react";

import DropdownWrapper from "components/forms/fields/DropdownWrapper";

const baseClass = "coverage-filter";

// Coverage filter values map to the `coverage` URL param and are translated to the /hosts/coverage API
// params by hostsAPI.getHostsByCoverage. "problems" = any gap/at-risk; "missing:<cat>" = no fresh
// protected cell for that category.
export const COVERAGE_FILTER_OPTIONS = [
  { label: "All hosts", value: "" },
  {
    label: "Only problems",
    value: "problems",
    helpText: "Any coverage gap or at-risk cell",
  },
  { label: "Missing managed AV", value: "missing:av" },
  { label: "Missing MDR / EDR", value: "missing:mdr" },
  { label: "Missing patching", value: "missing:patching" },
  { label: "Missing backups", value: "missing:backups" },
  { label: "Missing remote access", value: "missing:remote_access" },
];

interface ICoverageFilterProps {
  value?: string;
  onChange: (value: string) => void;
  isDisabled?: boolean;
}

const CoverageFilter = ({
  value,
  onChange,
  isDisabled,
}: ICoverageFilterProps): JSX.Element => {
  return (
    <DropdownWrapper
      name="coverage-filter"
      className={baseClass}
      value={value || ""}
      options={COVERAGE_FILTER_OPTIONS}
      onChange={(newValue: { value: string } | null) =>
        onChange(newValue?.value ?? "")
      }
      variant="table-filter"
      isDisabled={isDisabled}
    />
  );
};

export default CoverageFilter;
