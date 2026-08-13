import { defineConfig } from "@playwright/test";

/**
 * Post-Deployment Verification (PDV) configuration.
 * Runs against the live deployed site to verify content and functionality.
 * These tests are BLOCKING — a failure means the deployment is broken.
 */
export default defineConfig({
  testDir: "./e2e",
  testMatch: "pdv.spec.ts",
  use: {
    baseURL: process.env.SITE_URL || "https://prometheus_exporters.phaseshiftdata.com",
    // Wait for SPA JavaScript to render
    actionTimeout: 10000,
  },
  retries: 2,
  timeout: 30000,
});
