import { defineConfig } from "@playwright/test";

/**
 * Playwright config for instrumented coverage runs.
 * Builds with INSTRUMENT_COVERAGE=1 to enable vite-plugin-istanbul,
 * then serves the instrumented build and runs e2e specs.
 *
 * Coverage JSON is collected via the coverage-fixture and merged
 * with Vitest LCOV by scripts/merge-coverage.sh.
 */
export default defineConfig({
  testDir: "./e2e",
  testMatch: "site.coverage.spec.ts",
  use: {
    baseURL: "http://localhost:4173",
  },
  webServer: {
    command: "INSTRUMENT_COVERAGE=1 npx vite build && npx vite preview --port 4173",
    port: 4173,
    reuseExistingServer: false,
  },
});
