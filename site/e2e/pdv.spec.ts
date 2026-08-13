import { test, expect } from "@playwright/test";

/**
 * Post-Deployment Verification (PDV) tests.
 * These run against the live deployed site after GitHub Pages deployment
 * to verify the website is up and all features and content are correct.
 */

test.describe("post-deployment verification", () => {
  test("site is reachable and returns 200", async ({ request }) => {
    const response = await request.get("/");
    expect(response.status()).toBe(200);
  });

  test("home page renders with correct structure", async ({ page }) => {
    await page.goto("/");
    await expect(page.locator(".nav-brand")).toContainText("prometheus_exporters");
    await expect(page.locator(".hero h1")).toContainText("Prometheus Exporters");
    await expect(page.locator(".hero-badges")).toBeVisible();
    await expect(page.locator(".footer-inner")).toBeVisible();
  });

  test("home page contains all three exporters", async ({ page }) => {
    await page.goto("/");
    await expect(page.locator("body")).toContainText("network_exporter");
    await expect(page.locator("body")).toContainText("ipsec_exporter");
    await expect(page.locator("body")).toContainText("cloudflare_exporter");
  });

  test("navigation links are present and functional", async ({ page }) => {
    await page.goto("/");

    // Verify nav links exist
    await expect(page.locator('a[href="#/network-exporter"]')).toBeVisible();
    await expect(page.locator('a[href="#/ipsec-exporter"]')).toBeVisible();
    await expect(page.locator('a[href="#/cloudflare-exporter"]')).toBeVisible();

    // Navigate to each page
    await page.click('a[href="#/cloudflare-exporter"]');
    await expect(page.locator(".main")).toContainText("Cloudflare Exporter");

    await page.click('a[href="#/network-exporter"]');
    await expect(page.locator(".main")).toContainText("Network Exporter");

    await page.click('a[href="#/ipsec-exporter"]');
    await expect(page.locator(".main")).toContainText("IPsec Exporter");
  });

  test("cloudflare exporter page has configuration documentation", async ({ page }) => {
    await page.goto("/#/cloudflare-exporter");
    await expect(page.locator(".main")).toContainText("CF_API_TOKEN");
    await expect(page.locator(".main")).toContainText("CF_SCRAPE_DELAY_SECONDS");
    await expect(page.locator(".main")).toContainText("LISTEN_ADDRESS");
  });

  test("cloudflare exporter page has metrics catalog", async ({ page }) => {
    await page.goto("/#/cloudflare-exporter");
    await expect(page.locator(".main")).toContainText("cloudflare_access_login_requests_total");
    await expect(page.locator(".main")).toContainText("cloudflare_gateway_dns_queries_total");
    await expect(page.locator(".main")).toContainText("cloudflare_domain_expiration_timestamp_seconds");
  });

  test("cloudflare exporter page has installation instructions", async ({ page }) => {
    await page.goto("/#/cloudflare-exporter");
    await expect(page.locator(".main")).toContainText("docker");
    await expect(page.locator(".main")).toContainText("ghcr.io/asymmetric-effort/cloudflare_exporter");
  });

  test("footer contains correct links", async ({ page }) => {
    await page.goto("/");
    const footer = page.locator(".footer-inner");
    await expect(footer).toContainText("MIT License");
    await expect(footer).toContainText("Asymmetric Effort");
    await expect(footer.locator('a[href*="github.com"]')).toBeVisible();
    await expect(footer.locator('a[href*="SECURITY.md"]')).toBeVisible();
    await expect(footer.locator('a[href*="CONTRIBUTING.md"]')).toBeVisible();
  });

  test("favicon is served", async ({ request }) => {
    const response = await request.get("/favicon.ico");
    expect(response.status()).toBe(200);
  });

  test("robots.txt is served with correct content", async ({ request }) => {
    const response = await request.get("/robots.txt");
    expect(response.status()).toBe(200);
    const body = await response.text();
    expect(body).toContain("User-agent");
    expect(body).toContain("Sitemap");
  });

  test("sitemap.xml is served with correct content", async ({ request }) => {
    const response = await request.get("/sitemap.xml");
    expect(response.status()).toBe(200);
    const body = await response.text();
    expect(body).toContain("urlset");
    expect(body).toContain("prometheus_exporters.asymmetric-effort.com");
  });

  test("CNAME file is served", async ({ request }) => {
    const response = await request.get("/CNAME");
    expect(response.status()).toBe(200);
    const body = await response.text();
    expect(body.trim()).toBe("prometheus_exporters.asymmetric-effort.com");
  });

  test("404 page redirects to root", async ({ page }) => {
    await page.goto("/nonexistent-page");
    await expect(page.locator(".nav-brand")).toBeVisible();
  });

  test("page title updates on navigation", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveTitle(/Prometheus Exporters/);

    await page.click('a[href="#/cloudflare-exporter"]');
    await expect(page).toHaveTitle(/Cloudflare Exporter/);
  });

  test("all code blocks are properly formatted", async ({ page }) => {
    await page.goto("/#/cloudflare-exporter");
    const codeBlocks = page.locator("pre code");
    const count = await codeBlocks.count();
    expect(count).toBeGreaterThan(0);

    // Verify code blocks have content
    for (let i = 0; i < count; i++) {
      const text = await codeBlocks.nth(i).textContent();
      expect(text?.trim().length).toBeGreaterThan(0);
    }
  });

  test("all tables have headers and content", async ({ page }) => {
    await page.goto("/#/cloudflare-exporter");
    const tables = page.locator("table");
    const count = await tables.count();
    expect(count).toBeGreaterThan(0);

    for (let i = 0; i < count; i++) {
      const headers = tables.nth(i).locator("th");
      const headerCount = await headers.count();
      expect(headerCount).toBeGreaterThan(0);

      const rows = tables.nth(i).locator("tbody tr");
      const rowCount = await rows.count();
      expect(rowCount).toBeGreaterThan(0);
    }
  });

  test("version is displayed in footer", async ({ page }) => {
    await page.goto("/");
    const footer = page.locator(".footer-inner");
    await expect(footer).toContainText(/v\d+\.\d+\.\d+/);
  });
});
