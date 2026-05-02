/**
 * Vitest test setup — imports jest-dom matchers for DOM assertions.
 *
 * Purpose:
 *   Extends Vitest's expect with @testing-library/jest-dom matchers
 *   so tests can use toBeInTheDocument(), toHaveClass(), etc.
 *
 * Related files:
 *   - vitest.config.ts (references this file as setupFiles)
 *   - src/__tests__/ (all test files benefit from these matchers)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

import "@testing-library/jest-dom";
