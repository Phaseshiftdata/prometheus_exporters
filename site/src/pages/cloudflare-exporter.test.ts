import { describe, it, expect } from "vitest";
import { CloudflareExporterPage } from "./cloudflare-exporter.js";

describe("CloudflareExporterPage", () => {
  it("returns HTML containing the page title", () => {
    const html = CloudflareExporterPage();
    expect(html).toContain("Cloudflare Exporter");
  });

  it("contains configuration variables", () => {
    const html = CloudflareExporterPage();
    expect(html).toContain("CF_API_TOKEN");
    expect(html).toContain("CF_SCRAPE_DELAY_SECONDS");
    expect(html).toContain("LISTEN_ADDRESS");
    expect(html).toContain("CF_GRAPHQL_BUDGET_PER_WINDOW");
    expect(html).toContain("CF_REST_BUDGET_PER_WINDOW");
  });

  it("contains Zero Trust metrics", () => {
    const html = CloudflareExporterPage();
    expect(html).toContain("access_login_requests_total");
    expect(html).toContain("gateway_dns_queries_total");
    expect(html).toContain("gateway_network_sessions_total");
    expect(html).toContain("tunnel_requests_total");
  });

  it("contains DNS metrics", () => {
    const html = CloudflareExporterPage();
    expect(html).toContain("dns_queries_total");
    expect(html).toContain("dns_query_duration_seconds");
  });

  it("contains domain and certificate metrics", () => {
    const html = CloudflareExporterPage();
    expect(html).toContain("domain_expiration_timestamp_seconds");
    expect(html).toContain("certificate_expiration_timestamp_seconds");
  });

  it("contains self-instrumentation metrics", () => {
    const html = CloudflareExporterPage();
    expect(html).toContain("cloudflare_exporter_build_info");
    expect(html).toContain("cloudflare_exporter_scrape_duration_seconds");
  });

  it("contains architecture section", () => {
    const html = CloudflareExporterPage();
    expect(html).toContain("Architecture");
    expect(html).toContain("Capability Discovery");
    expect(html).toContain("Quota Governor");
    expect(html).toContain("Aggregation Store");
  });

  it("contains installation instructions", () => {
    const html = CloudflareExporterPage();
    expect(html).toContain("ghcr.io/phaseshiftdata/cloudflare_exporter");
    expect(html).toContain("docker");
  });

  it("contains API token setup section", () => {
    const html = CloudflareExporterPage();
    expect(html).toContain("API Token Setup");
    expect(html).toContain("not a Global API Key");
    expect(html).toContain("My Profile");
  });

  it("contains endpoints section", () => {
    const html = CloudflareExporterPage();
    expect(html).toContain("/metrics");
    expect(html).toContain("/health");
    expect(html).toContain("/capabilities");
  });

  it("contains secret configuration section", () => {
    const html = CloudflareExporterPage();
    expect(html).toContain("Secret Configuration");
    expect(html).toContain("api-token-file");
    expect(html).toContain("openbao");
  });

  it("contains collection model section", () => {
    const html = CloudflareExporterPage();
    expect(html).toContain("Collection Model");
    expect(html).toContain("five-minute");
    expect(html).toContain("double-counting");
  });

  it("contains failure modes section", () => {
    const html = CloudflareExporterPage();
    expect(html).toContain("Failure Modes");
    expect(html).toContain("Stale-but-served");
    expect(html).toContain("Budget exhaustion");
  });
});
