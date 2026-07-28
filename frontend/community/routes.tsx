import React from "react";
import { Route } from "react-router";

import CoverageGridPage from "./pages/CoverageGridPage";

/**
 * Community (fork) routes, spliced into the authenticated CoreLayout block in router/index.tsx via a
 * single `{communityRoutes}` hook — react-router v3 flattens interpolated element arrays, so each
 * entry behaves exactly like an inline <Route>. Every element needs a unique key.
 */
export const communityRoutes = [
  <Route key="coverage" path="coverage" component={CoverageGridPage} />,
];

export default communityRoutes;
