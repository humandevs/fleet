import URL_PREFIX from "router/url_prefix";

/**
 * Community (fork) page paths — the frontend mirror of server/community/.
 *
 * Spread into the core PATHS object via a single one-line hook in router/paths.ts, so community
 * pages register zero-per-page edits in core files. Community modules import THIS object (never
 * core PATHS) for community paths, which keeps the dependency direction one-way (core -> community)
 * and avoids a require cycle.
 */
export const communityPaths = {
  COVERAGE_GRID: `${URL_PREFIX}/coverage`,
} as const;

export default communityPaths;
