import { test, expect } from "@playwright/test";

test.describe("site deployment verification", () => {
  test("home page renders with correct structure", async ({ page }) => {
    await page.goto("/");
    await expect(page.locator(".nav-brand")).toContainText("prometheus_exporters");
    await expect(page.locator(".hero h1")).toContainText("Prometheus Exporters");
    await expect(page.locator(".hero-badges")).toBeVisible();
    await expect(page.locator(".footer-inner")).toBeVisible();
  });

  test("navigation works", async ({ page }) => {
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

    await page.click('a[href="#/openbao-exporter"]');
    await expect(page.locator(".main")).toContainText("OpenBao Exporter");

    await page.click('a[href="#/relay-exporter"]');
    await expect(page.locator(".main")).toContainText("Relay Exporter");
  });

  test("favicon.ico is served and returns valid response", async ({ request }) => {
    const response = await request.get("/favicon.ico");
    expect(response.status()).toBe(200);
    const body = await response.body();
    expect(body.length).toBeGreaterThan(0);
  });

  test("robots.txt is served", async ({ request }) => {
    const response = await request.get("/robots.txt");
    expect(response.status()).toBe(200);
  });

  test("sitemap.xml is served", async ({ request }) => {
    const response = await request.get("/sitemap.xml");
    expect(response.status()).toBe(200);
  });
});
