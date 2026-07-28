import { DOCUMENT_TITLE_SUFFIX } from "utilities/constants";

import { communityPaths } from "./paths";

/**
 * Community (fork) page titles, spread into core page_titles via a single hook. Spread FIRST in the
 * core array: title lookup uses array.find(), so specific paths must precede any parent path a core
 * entry might claim.
 */
export const communityPageTitles = [
  {
    path: communityPaths.COVERAGE_GRID,
    title: `Coverage | ${DOCUMENT_TITLE_SUFFIX}`,
  },
];

export default communityPageTitles;
