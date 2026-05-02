/**
 * Vitest configuration for radstorm web frontend.
 *
 * Purpose:
 *   Configures Vitest with jsdom environment and React plugin, sets up
 *   path aliases matching tsconfig, and points to the test setup file.
 *
 * Related files:
 *   - src/__tests__/ (test files)
 *   - src/test-setup.ts (jest-dom matchers)
 *   - tsconfig.json (path aliases mirrored here)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import path from "path";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test-setup.ts"],
    include: ["src/**/*.test.{ts,tsx}", "src/**/*.spec.{ts,tsx}"],
    exclude: ["node_modules", ".next"],
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
});
