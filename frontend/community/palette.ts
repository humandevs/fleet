import type {
  ICommandItem,
  ICommandPaletteContext,
} from "components/CommandPalette/helpers";

import { communityPaths } from "./paths";

/**
 * Community (fork) command-palette items, spread into buildPaletteItems via a single hook —
 * the same composition pattern as the core ./groups/ builders (type-only imports from helpers,
 * so the load-time cycle is erased exactly like groups/pages.ts).
 */
export const buildCommunityItems = (
  ctx: ICommandPaletteContext
): ICommandItem[] => {
  const { withTeamId } = ctx;
  return [
    {
      id: "coverage-grid",
      label: "Coverage",
      group: "Pages" as const,
      path: withTeamId(communityPaths.COVERAGE_GRID),
      keywords: [
        "coverage",
        "integrations",
        "av",
        "antivirus",
        "screenconnect",
        "remote access",
        "patch",
        "grid",
        "rmm",
        "problem devices",
      ],
    },
  ];
};

export default buildCommunityItems;
