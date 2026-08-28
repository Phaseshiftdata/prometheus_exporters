import { test as base, expect } from "@playwright/test";
import * as fs from "fs";
import * as path from "path";
import * as crypto from "crypto";

/**
 * Extended Playwright test fixture that collects istanbul coverage
 * from window.__coverage__ after each test. Only active when
 * INSTRUMENT_COVERAGE=1 is set.
 */
export const test = base.extend({
  page: async ({ page }, use) => {
    await use(page);

    // Only collect coverage when instrumentation is enabled
    if (!process.env.INSTRUMENT_COVERAGE) return;

    try {
      const coverageJSON = await page.evaluate(() => {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const cov = (window as any).__coverage__;
        return cov ? JSON.stringify(cov) : null;
      });

      if (coverageJSON) {
        // Use process.cwd() which will be the site/ directory
        const coverageDir = path.join(process.cwd(), "coverage", "e2e");
        fs.mkdirSync(coverageDir, { recursive: true });

        const id = crypto.randomBytes(8).toString("hex");
        const filePath = path.join(coverageDir, `coverage-${id}.json`);
        fs.writeFileSync(filePath, coverageJSON);
      }
    } catch (err) {
      // Log errors during coverage collection for debugging
      console.warn("Coverage collection warning:", (err as Error).message);
    }
  },
});

export { expect };
