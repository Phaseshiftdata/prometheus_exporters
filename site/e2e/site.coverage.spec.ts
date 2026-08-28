import { test, expect } from "./coverage-fixture.js";

/**
 * Coverage-instrumented e2e tests. These mirror site.spec.ts but use the
 * coverage fixture to extract window.__coverage__ after each test.
 *
 * Run with: INSTRUMENT_COVERAGE=1 npx playwright test --config=playwright.coverage.config.ts
 */

test.describe("site coverage e2e", () => {
  test("home page renders with correct structure", async ({ page }) => {
    await page.goto("/");
    await expect(page.locator(".nav-brand")).toContainText("prometheus_exporters");
    await expect(page.locator(".hero h1")).toContainText("Prometheus Exporters");
    await expect(page.locator(".hero-badges")).toBeVisible();
    await expect(page.locator(".footer-inner")).toBeVisible();
  });

  test("navigation works across all pages", async ({ page }) => {
    await page.goto("/");

    await page.click('a[href="#/network-exporter"]');
    await expect(page.locator(".main")).toContainText("Network Exporter");

    await page.click('a[href="#/ipsec-exporter"]');
    await expect(page.locator(".main")).toContainText("IPsec Exporter");

    await page.click('a[href="#/cloudflare-exporter"]');
    await expect(page.locator(".main")).toContainText("Cloudflare Exporter");

    await page.click('a[href="#/github-exporter"]');
    await expect(page.locator(".main")).toContainText("GitHub Exporter");

    await page.click('a[href="#/libvirt-exporter"]');
    await expect(page.locator(".main")).toContainText("Libvirt Exporter");

    await page.click('a[href="#/relay-exporter"]');
    await expect(page.locator(".main")).toContainText("Relay Exporter");

    // Navigate back to home
    await page.click('a[href="#/"]');
    await expect(page.locator(".hero h1")).toContainText("Prometheus Exporters");
  });

  test("page title updates on navigation", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveTitle(/Prometheus Exporters/);

    await page.click('a[href="#/cloudflare-exporter"]');
    await expect(page).toHaveTitle(/Cloudflare Exporter/);

    await page.click('a[href="#/network-exporter"]');
    await expect(page).toHaveTitle(/Network Exporter/);
  });

  test("direct hash navigation works", async ({ page }) => {
    await page.goto("/#/github-exporter");
    await expect(page.locator(".main")).toContainText("GitHub Exporter");
  });

  test("unknown route falls back to home", async ({ page }) => {
    await page.goto("/#/nonexistent");
    await expect(page.locator(".hero h1")).toContainText("Prometheus Exporters");
  });

  test("footer contains version and license", async ({ page }) => {
    await page.goto("/");
    const footer = page.locator(".footer-inner");
    await expect(footer).toContainText(/v\d+\.\d+\.\d+/);
    await expect(footer).toContainText("MIT License");
    await expect(footer).toContainText("Asymmetric Effort");
  });

  test("footer contains external links", async ({ page }) => {
    await page.goto("/");
    const footer = page.locator(".footer-inner");
    await expect(footer.locator('a[href*="github.com"]').first()).toBeVisible();
    await expect(footer.locator('a[href*="SECURITY.md"]').first()).toBeVisible();
    await expect(footer.locator('a[href*="CONTRIBUTING.md"]').first()).toBeVisible();
  });

  test("active nav link changes on navigation", async ({ page }) => {
    await page.goto("/");
    await expect(page.locator('a.active:text("Home")')).toBeVisible();

    await page.click('a[href="#/relay-exporter"]');
    await expect(page.locator('a.active:text("Relay")')).toBeVisible();
  });
});
