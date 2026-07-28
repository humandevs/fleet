import URL_PREFIX from "router/url_prefix";
import type { INavItem } from "components/top_nav/SiteTopNav/navItems";

import { communityPaths } from "./paths";

/**
 * Community (fork) top-nav items, spread into the core navItems array via a single hook. A function
 * (not a static array) from day one so a future community page can gate on role flags without
 * changing the hook's shape; the core exclude-filter applies to spread items like any other.
 */
export const communityNavItems = (): INavItem[] => [
  {
    name: "Coverage",
    location: {
      regex: new RegExp(`^${URL_PREFIX}/coverage`),
      pathname: communityPaths.COVERAGE_GRID,
    },
    withParams: { type: "query", names: ["fleet_id"] },
  },
];

export default communityNavItems;
