import { defineConfig } from "@playwright/test";

/**
 * Post-Deployment Verification (PDV) configuration.
 * Runs against the live deployed site to verify content and functionality.
 * These tests are BLOCKING — a failure means the deployment is broken.
 *
 * Retries are set to 3 with a backoff because GitHub Pages CDN caching
 * can serve stale content for up to 10 minutes after a deployment.
 */
export default defineConfig({
  testDir: "./e2e",
  testMatch: "pdv.spec.ts",
  use: {
    baseURL: process.env.SITE_URL || "https://prometheus_exporters.phaseshiftdata.com",
    actionTimeout: 10000,
  },
  retries: 3,
  timeout: 30000,
});
