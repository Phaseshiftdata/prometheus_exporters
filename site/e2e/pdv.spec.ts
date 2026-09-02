import { test, expect } from "@playwright/test";

/**
 * Post-Deployment Verification (PDV) tests.
 * These run against the live deployed site after GitHub Pages deployment
 * to verify the website is up and all features and content are correct.
 */

test.describe("post-deployment verification", () => {
  // Some tests only work at root level, not /staging/ subdirectory.
  const isStaging = (process.env.SITE_URL || "").includes("/staging");

  test("site is reachable and returns 200", async ({ request }) => {
    const response = await request.get("./");
    expect(response.status()).toBe(200);
  });

  test("home page renders with correct structure", async ({ page }) => {
    await page.goto("./");
    await expect(page.locator(".nav-brand")).toContainText("prometheus_exporters");
    await expect(page.locator(".hero h1")).toContainText("Prometheus Exporters");
    await expect(page.locator(".hero-badges")).toBeVisible();
    await expect(page.locator(".footer-inner")).toBeVisible();
  });

  test("home page contains all exporters", async ({ page }) => {
    await page.goto("./");
    await expect(page.locator("body")).toContainText("network_exporter");
    await expect(page.locator("body")).toContainText("ipsec_exporter");
    await expect(page.locator("body")).toContainText("cloudflare_exporter");
    await expect(page.locator("body")).toContainText("libvirt_exporter");
    await expect(page.locator("body")).toContainText("relay_exporter");
  });

  test("navigation links are present and functional", async ({ page }) => {
    await page.goto("./");

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
    await page.goto("./#/cloudflare-exporter");
    await expect(page.locator(".main")).toContainText("CF_API_TOKEN");
    await expect(page.locator(".main")).toContainText("CF_SCRAPE_DELAY_SECONDS");
    await expect(page.locator(".main")).toContainText("LISTEN_ADDRESS");
  });

  test("cloudflare exporter page has metrics catalog", async ({ page }) => {
    await page.goto("./#/cloudflare-exporter");
    await expect(page.locator(".main")).toContainText("access_login_requests_total");
    await expect(page.locator(".main")).toContainText("gateway_dns_queries_total");
    await expect(page.locator(".main")).toContainText("domain_expiration_timestamp_seconds");
  });

  test("cloudflare exporter page has installation instructions", async ({ page }) => {
    await page.goto("./#/cloudflare-exporter");
    await expect(page.locator(".main")).toContainText("docker");
    await expect(page.locator(".main")).toContainText("ghcr.io/phaseshiftdata/cloudflare_exporter");
  });

  test("footer contains correct links", async ({ page }) => {
    await page.goto("./");
    const footer = page.locator(".footer-inner");
    await expect(footer).toContainText("MIT License");
    await expect(footer).toContainText("Asymmetric Effort");
    await expect(footer.locator('a[href*="github.com"]').first()).toBeVisible();
    await expect(footer.locator('a[href*="SECURITY.md"]').first()).toBeVisible();
    await expect(footer.locator('a[href*="CONTRIBUTING.md"]').first()).toBeVisible();
  });

  test("favicon is served", async ({ request }) => {
    const response = await request.get("./favicon.ico");
    expect(response.status()).toBe(200);
  });

  test("robots.txt is served with correct content", async ({ request }) => {
    const response = await request.get("./robots.txt");
    expect(response.status()).toBe(200);
    const body = await response.text();
    expect(body).toContain("User-agent");
    expect(body).toContain("Sitemap");
  });

  test("sitemap.xml is served with correct content", async ({ request }) => {
    const response = await request.get("./sitemap.xml");
    expect(response.status()).toBe(200);
    const body = await response.text();
    expect(body).toContain("urlset");
    expect(body).toContain("prometheus_exporters.phaseshiftdata.com");
  });

  // CNAME only exists at root level, not in /staging/ subdirectory.
  (isStaging ? test.skip : test)("CNAME file is served", async ({ request }) => {
    const response = await request.get("./CNAME");
    expect(response.status()).toBe(200);
    const body = await response.text();
    expect(body.trim()).toBe("prometheus_exporters.phaseshiftdata.com");
  });

  // 404 redirect only works at root level (GitHub Pages 404.html);
  // skip when running against /staging/ subdirectory.
  (isStaging ? test.skip : test)("404 page redirects to root", async ({ page }) => {
    await page.goto("/nonexistent-page");
    await expect(page.locator(".nav-brand")).toBeVisible();
  });

  test("page title updates on navigation", async ({ page }) => {
    await page.goto("./");
    await expect(page).toHaveTitle(/Prometheus Exporters/);

    await page.click('a[href="#/cloudflare-exporter"]');
    await expect(page).toHaveTitle(/Cloudflare Exporter/);
  });

  test("all code blocks are properly formatted", async ({ page }) => {
    await page.goto("./#/cloudflare-exporter");
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
    await page.goto("./#/cloudflare-exporter");
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
    await page.goto("./");
    const footer = page.locator(".footer-inner");
    await expect(footer).toContainText(/v\d+\.\d+\.\d+/);
  });

  // Network exporter page coverage
  test("network exporter page renders with content", async ({ page }) => {
    await page.goto("./#/network-exporter");
    await expect(page.locator(".main")).toContainText("Network Exporter");
    await expect(page.locator(".main")).toContainText("ghcr.io/phaseshiftdata/network_exporter");
    await expect(page.locator("pre code").first()).toBeVisible();
  });

  test("network exporter page has configuration documentation", async ({ page }) => {
    await page.goto("./#/network-exporter");
    await expect(page.locator(".main")).toContainText("--listen-address");
    await expect(page.locator(".main")).toContainText("--proc-path");
    await expect(page.locator(".main")).toContainText("--sys-path");
    await expect(page.locator(".main")).toContainText("--log-level");
  });

  test("network exporter page has collector documentation", async ({ page }) => {
    await page.goto("./#/network-exporter");
    await expect(page.locator(".main")).toContainText("Metrics: ARP");
    await expect(page.locator(".main")).toContainText("Metrics: Interface");
    await expect(page.locator(".main")).toContainText("Metrics: Network Graph");
    await expect(page.locator(".main")).toContainText("Metrics: Conntrack");
    await expect(page.locator(".main")).toContainText("Metrics: Firewall");
  });

  test("network exporter page has metrics catalog", async ({ page }) => {
    await page.goto("./#/network-exporter");
    await expect(page.locator(".main")).toContainText("network_arp_entry");
    await expect(page.locator(".main")).toContainText("network_interface_type");
    await expect(page.locator(".main")).toContainText("network_graph_edge");
    await expect(page.locator(".main")).toContainText("network_port_connections");
    await expect(page.locator(".main")).toContainText("network_firewall_drop_packets_total");
    await expect(page.locator(".main")).toContainText("network_firewall_collector_up");
    await expect(page.locator(".main")).toContainText("network_conntrack_accounting_enabled");
  });

  test("network exporter page has architecture section", async ({ page }) => {
    await page.goto("./#/network-exporter");
    await expect(page.locator(".main")).toContainText("Architecture");
    await expect(page.locator(".main")).toContainText("NETLINK_ROUTE");
    await expect(page.locator(".main")).toContainText("NETLINK_NETFILTER");
    await expect(page.locator(".main")).toContainText("procfs");
    await expect(page.locator(".main")).toContainText("sysfs");
  });

  test("network exporter page has deployment section", async ({ page }) => {
    await page.goto("./#/network-exporter");
    await expect(page.locator(".main")).toContainText("DaemonSet");
    await expect(page.locator(".main")).toContainText("CAP_NET_ADMIN");
    await expect(page.locator(".main")).toContainText("hostPID");
    await expect(page.locator(".main")).toContainText("hostNetwork");
  });

  test("network exporter page has correct docker run requirements", async ({ page }) => {
    await page.goto("./#/network-exporter");
    await expect(page.locator(".main")).toContainText("--network=host");
    await expect(page.locator(".main")).toContainText("--pid=host");
    await expect(page.locator(".main")).toContainText("--user 0:0");
    await expect(page.locator(".main")).toContainText("--cap-add=NET_ADMIN");
    await expect(page.locator(".main")).toContainText("--cap-add=DAC_READ_SEARCH");
    await expect(page.locator(".main")).toContainText("--security-opt label=disable");
    await expect(page.locator(".main")).toContainText("/host/proc");
    await expect(page.locator(".main")).toContainText("/host/sys");
  });

  test("network exporter page has failure modes section", async ({ page }) => {
    await page.goto("./#/network-exporter");
    await expect(page.locator(".main")).toContainText("Failure Modes");
    await expect(page.locator(".main")).toContainText("ContinueOnError");
    await expect(page.locator(".main")).toContainText("nftables not available");
  });

  // IPsec exporter page coverage
  test("ipsec exporter page renders with content", async ({ page }) => {
    await page.goto("./#/ipsec-exporter");
    await expect(page.locator(".main")).toContainText("IPsec Exporter");
    await expect(page.locator(".main")).toContainText("ghcr.io/phaseshiftdata/ipsec_exporter");
    await expect(page.locator("pre code").first()).toBeVisible();
  });

  test("ipsec exporter page has VICI socket documentation", async ({ page }) => {
    await page.goto("./#/ipsec-exporter");
    await expect(page.locator(".main")).toContainText("VICI");
    await expect(page.locator(".main")).toContainText("/var/run/charon.vici");
    await expect(page.locator(".main")).toContainText("strongSwan");
    await expect(page.locator(".main")).toContainText("list-sas");
    await expect(page.locator(".main")).toContainText("govici");
  });

  test("ipsec exporter page has IPsec metrics catalog", async ({ page }) => {
    await page.goto("./#/ipsec-exporter");
    await expect(page.locator(".main")).toContainText("ipsec_up");
    await expect(page.locator(".main")).toContainText("ipsec_ike_sa_state");
    await expect(page.locator(".main")).toContainText("ipsec_child_sa_bytes_in");
    await expect(page.locator(".main")).toContainText("ipsec_child_sa_bytes_out");
    await expect(page.locator(".main")).toContainText("ipsec_uptime_seconds");
    await expect(page.locator(".main")).toContainText("ipsec_workers_total");
    await expect(page.locator(".main")).toContainText("ipsec_queues");
  });

  test("ipsec exporter page has configuration flags", async ({ page }) => {
    await page.goto("./#/ipsec-exporter");
    await expect(page.locator(".main")).toContainText("--listen-address");
    await expect(page.locator(".main")).toContainText("--proc-path");
    await expect(page.locator(".main")).toContainText("--sys-path");
    await expect(page.locator(".main")).toContainText("--vici-socket");
    await expect(page.locator(".main")).toContainText("--log-level");
  });

  test("ipsec exporter page has SA state tables", async ({ page }) => {
    await page.goto("./#/ipsec-exporter");
    await expect(page.locator(".main")).toContainText("ESTABLISHED");
    await expect(page.locator(".main")).toContainText("CONNECTING");
    await expect(page.locator(".main")).toContainText("INSTALLED");
    await expect(page.locator(".main")).toContainText("REKEYED");
  });

  test("ipsec exporter page has failure modes section", async ({ page }) => {
    await page.goto("./#/ipsec-exporter");
    await expect(page.locator(".main")).toContainText("VICI socket unavailable");
    await expect(page.locator(".main")).toContainText("ContinueOnError");
    await expect(page.locator(".main")).toContainText("ipsec_up 0");
  });

  test("ipsec exporter page has correct docker run requirements", async ({ page }) => {
    await page.goto("./#/ipsec-exporter");
    await expect(page.locator(".main")).toContainText("--network=host");
    await expect(page.locator(".main")).toContainText("--pid=host");
    await expect(page.locator(".main")).toContainText("--user 0:0");
    await expect(page.locator(".main")).toContainText("--cap-add=NET_ADMIN");
    await expect(page.locator(".main")).toContainText("--cap-add=DAC_READ_SEARCH");
    await expect(page.locator(".main")).toContainText("--security-opt label=disable");
    await expect(page.locator(".main")).toContainText("/host/proc");
    await expect(page.locator(".main")).toContainText("/host/sys");
    await expect(page.locator(".main")).toContainText("/var/run/charon.vici");
  });

  test("ipsec exporter page has tunnel auto-discovery section", async ({ page }) => {
    await page.goto("./#/ipsec-exporter");
    await expect(page.locator(".main")).toContainText("Tunnel Auto-Discovery");
    await expect(page.locator(".main")).toContainText("uid");
  });

  test("ipsec exporter page has network metrics", async ({ page }) => {
    await page.goto("./#/ipsec-exporter");
    await expect(page.locator(".main")).toContainText("network_arp_entry");
    await expect(page.locator(".main")).toContainText("network_firewall_drop_packets_total");
    await expect(page.locator(".main")).toContainText("network_port_connections");
  });

  // Cloudflare exporter page comprehensive content checks
  test("cloudflare exporter page has architecture section", async ({ page }) => {
    await page.goto("./#/cloudflare-exporter");
    await expect(page.locator(".main")).toContainText("Capability Discovery");
    await expect(page.locator(".main")).toContainText("Quota Governor");
    await expect(page.locator(".main")).toContainText("Aggregation Store");
  });

  test("cloudflare exporter page has API token section", async ({ page }) => {
    await page.goto("./#/cloudflare-exporter");
    await expect(page.locator(".main")).toContainText("API Token Setup");
    await expect(page.locator(".main")).toContainText("not a Global API Key");
    await expect(page.locator(".main")).toContainText("My Profile");
  });

  test("cloudflare exporter page has endpoints section", async ({ page }) => {
    await page.goto("./#/cloudflare-exporter");
    await expect(page.locator(".main")).toContainText("/metrics");
    await expect(page.locator(".main")).toContainText("/health");
    await expect(page.locator(".main")).toContainText("/capabilities");
  });

  test("cloudflare exporter page has secret configuration section", async ({ page }) => {
    await page.goto("./#/cloudflare-exporter");
    await expect(page.locator(".main")).toContainText("Secret Configuration");
    await expect(page.locator(".main")).toContainText("api-token-file");
    await expect(page.locator(".main")).toContainText("openbao");
  });

  test("cloudflare exporter page has self-instrumentation metrics", async ({ page }) => {
    await page.goto("./#/cloudflare-exporter");
    await expect(page.locator(".main")).toContainText("cloudflare_exporter_build_info");
    await expect(page.locator(".main")).toContainText("cloudflare_exporter_scrape_duration_seconds");
  });

  test("cloudflare exporter page has collection model section", async ({ page }) => {
    await page.goto("./#/cloudflare-exporter");
    await expect(page.locator(".main")).toContainText("Collection Model");
    await expect(page.locator(".main")).toContainText("five-minute");
    await expect(page.locator(".main")).toContainText("double-counting");
  });

  // Home page comprehensive content checks
  test("home page has container image registry info", async ({ page }) => {
    await page.goto("./");
    await expect(page.locator("body")).toContainText("ghcr.io/phaseshiftdata");
    await expect(page.locator("body")).toContainText(":main");
    await expect(page.locator("body")).toContainText("commit-sha");
  });

  test("home page has all exporter ports", async ({ page }) => {
    await page.goto("./");
    await expect(page.locator("body")).toContainText("9101");
    await expect(page.locator("body")).toContainText("9102");
    await expect(page.locator("body")).toContainText("9199");
    await expect(page.locator("body")).toContainText("9177");
  });

  // GitHub exporter page coverage
  test("github exporter page renders with content", async ({ page }) => {
    await page.goto("./#/github-exporter");
    await expect(page.locator(".main")).toContainText("GitHub Exporter");
    await expect(page.locator(".main")).toContainText("ghcr.io/phaseshiftdata/github_exporter");
    await expect(page.locator("pre code").first()).toBeVisible();
  });

  test("github exporter page has configuration documentation", async ({ page }) => {
    await page.goto("./#/github-exporter");
    await expect(page.locator(".main")).toContainText("--github-app-id");
    await expect(page.locator(".main")).toContainText("--poll-interval");
    await expect(page.locator(".main")).toContainText("--listen-address");
    await expect(page.locator(".main")).toContainText("--database-url");
  });

  test("github exporter page has metrics catalog", async ({ page }) => {
    await page.goto("./#/github-exporter");
    await expect(page.locator(".main")).toContainText("github_exporter_workflow_runs_total");
    await expect(page.locator(".main")).toContainText("github_exporter_open_pull_requests");
    await expect(page.locator(".main")).toContainText("github_exporter_rate_limit_remaining");
    await expect(page.locator(".main")).toContainText("github_exporter_backfill_pending_job_runs");
  });

  test("github exporter page has installation instructions", async ({ page }) => {
    await page.goto("./#/github-exporter");
    await expect(page.locator(".main")).toContainText("docker");
    await expect(page.locator(".main")).toContainText("ghcr.io/phaseshiftdata/github_exporter");
  });

  test("github exporter page has architecture section", async ({ page }) => {
    await page.goto("./#/github-exporter");
    await expect(page.locator(".main")).toContainText("Poller");
    await expect(page.locator(".main")).toContainText("Backfiller");
    await expect(page.locator(".main")).toContainText("Rate Limit Manager");
    await expect(page.locator(".main")).toContainText("PostgreSQL Store");
  });

  test("github exporter page has GitHub App setup section", async ({ page }) => {
    await page.goto("./#/github-exporter");
    await expect(page.locator(".main")).toContainText("GitHub App Setup");
    await expect(page.locator(".main")).toContainText("GitHub App installation");
    await expect(page.locator(".main")).toContainText("5,000 requests per hour");
  });

  test("github exporter page has database section", async ({ page }) => {
    await page.goto("./#/github-exporter");
    await expect(page.locator(".main")).toContainText("Database");
    await expect(page.locator(".main")).toContainText("PostgreSQL 14");
    await expect(page.locator(".main")).toContainText("90 days");
  });

  test("github exporter page has rate limiting section", async ({ page }) => {
    await page.goto("./#/github-exporter");
    await expect(page.locator(".main")).toContainText("Rate Limiting");
    await expect(page.locator(".main")).toContainText("Primary");
    await expect(page.locator(".main")).toContainText("Secondary");
  });

  test("github exporter page has endpoints section", async ({ page }) => {
    await page.goto("./#/github-exporter");
    await expect(page.locator(".main")).toContainText("/metrics");
  });

  test("github exporter page has secret configuration section", async ({ page }) => {
    await page.goto("./#/github-exporter");
    await expect(page.locator(".main")).toContainText("Secret Configuration");
    await expect(page.locator(".main")).toContainText("database-password-file");
    await expect(page.locator(".main")).toContainText("openbao");
  });

  test("github exporter page has backfill metrics", async ({ page }) => {
    await page.goto("./#/github-exporter");
    await expect(page.locator(".main")).toContainText("github_exporter_backfill_last_step_timestamp_seconds");
    await expect(page.locator(".main")).toContainText("github_exporter_api_requests_total");
  });

  test("github exporter page has failure modes section", async ({ page }) => {
    await page.goto("./#/github-exporter");
    await expect(page.locator(".main")).toContainText("Failure Modes");
    await expect(page.locator(".main")).toContainText("Secondary rate limit");
    await expect(page.locator(".main")).toContainText("Database unavailable");
  });

  // Libvirt exporter page coverage
  test("navigation to libvirt page works", async ({ page }) => {
    await page.goto("./");
    await page.click('a[href="#/libvirt-exporter"]');
    await expect(page.locator(".main")).toContainText("Libvirt Exporter");
  });

  test("libvirt exporter page has config flags", async ({ page }) => {
    await page.goto("./#/libvirt-exporter");
    await expect(page.locator(".main")).toContainText("--libvirt-uri");
    await expect(page.locator(".main")).toContainText("--listen-address");
  });

  test("libvirt exporter page has key metrics", async ({ page }) => {
    await page.goto("./#/libvirt-exporter");
    await expect(page.locator(".main")).toContainText("libvirt_up");
    await expect(page.locator(".main")).toContainText("libvirt_domain_cpu_time_seconds_total");
  });

  test("home page lists libvirt_exporter and port 9177", async ({ page }) => {
    await page.goto("./");
    await expect(page.locator("body")).toContainText("libvirt_exporter");
    await expect(page.locator("body")).toContainText("9177");
  });

  // OpenBao exporter page coverage
  test("openbao exporter page renders with content", async ({ page }) => {
    await page.goto("./#/openbao-exporter");
    await expect(page.locator(".main")).toContainText("OpenBao Exporter");
    await expect(page.locator(".main")).toContainText("ghcr.io/phaseshiftdata/openbao_exporter");
    await expect(page.locator("pre code").first()).toBeVisible();
  });

  test("openbao exporter page has configuration flags", async ({ page }) => {
    await page.goto("./#/openbao-exporter");
    await expect(page.locator(".main")).toContainText("--listen-address");
    await expect(page.locator(".main")).toContainText("--openbao-addr");
    await expect(page.locator(".main")).toContainText("--openbao-token-file");
    await expect(page.locator(".main")).toContainText("--log-level");
    await expect(page.locator(".main")).toContainText("--poll-interval");
  });

  test("openbao exporter page has health metrics", async ({ page }) => {
    await page.goto("./#/openbao-exporter");
    await expect(page.locator(".main")).toContainText("openbao_up");
    await expect(page.locator(".main")).toContainText("openbao_initialized");
    await expect(page.locator(".main")).toContainText("openbao_sealed");
    await expect(page.locator(".main")).toContainText("openbao_standby");
    await expect(page.locator(".main")).toContainText("openbao_leader");
  });

  test("openbao exporter page has cluster metrics", async ({ page }) => {
    await page.goto("./#/openbao-exporter");
    await expect(page.locator(".main")).toContainText("openbao_raft_committed_index");
    await expect(page.locator(".main")).toContainText("openbao_peers");
  });

  test("openbao exporter page has architecture section", async ({ page }) => {
    await page.goto("./#/openbao-exporter");
    await expect(page.locator(".main")).toContainText("Architecture");
    await expect(page.locator(".main")).toContainText("Collection Flow");
  });

  test("openbao exporter page has cluster discovery section", async ({ page }) => {
    await page.goto("./#/openbao-exporter");
    await expect(page.locator(".main")).toContainText("Cluster Discovery");
    await expect(page.locator(".main")).toContainText("/v1/sys/storage/raft/configuration");
  });

  test("openbao exporter page has deployment section", async ({ page }) => {
    await page.goto("./#/openbao-exporter");
    await expect(page.locator(".main")).toContainText("Kubernetes");
    await expect(page.locator(".main")).toContainText("Docker Compose");
  });

  test("openbao exporter page has failure modes section", async ({ page }) => {
    await page.goto("./#/openbao-exporter");
    await expect(page.locator(".main")).toContainText("Failure Modes");
    await expect(page.locator(".main")).toContainText("Seed node unreachable");
  });

  test("openbao exporter page has security section", async ({ page }) => {
    await page.goto("./#/openbao-exporter");
    await expect(page.locator(".main")).toContainText("Security");
    await expect(page.locator(".main")).toContainText("Distroless container");
  });

  test("navigation to openbao exporter page works", async ({ page }) => {
    await page.goto("./");
    await page.click('a[href="#/openbao-exporter"]');
    await expect(page.locator(".main")).toContainText("OpenBao Exporter");
  });

  test("home page lists openbao_exporter", async ({ page }) => {
    await page.goto("./");
    await expect(page.locator("body")).toContainText("openbao_exporter");
  });

  // Relay exporter page coverage
  test("relay exporter page renders with content", async ({ page }) => {
    await page.goto("./#/relay-exporter");
    await expect(page.locator(".main")).toContainText("Relay Exporter");
    await expect(page.locator(".main")).toContainText("ghcr.io/phaseshiftdata/relay_exporter");
    await expect(page.locator("pre code").first()).toBeVisible();
  });

  test("relay exporter page has configuration flags", async ({ page }) => {
    await page.goto("./#/relay-exporter");
    await expect(page.locator(".main")).toContainText("--listen-address");
    await expect(page.locator(".main")).toContainText("--allowed-source");
    await expect(page.locator(".main")).toContainText("--tls-cert-file");
    await expect(page.locator(".main")).toContainText("--tls-key-file");
    await expect(page.locator(".main")).toContainText("--ca-cert");
    await expect(page.locator(".main")).toContainText("--tls-skip-verify");
    await expect(page.locator(".main")).toContainText("--proxy-timeout");
    await expect(page.locator(".main")).toContainText("--concurrent-requests");
    await expect(page.locator(".main")).toContainText("--log-level");
  });

  test("relay exporter page has response format documentation", async ({ page }) => {
    await page.goto("./#/relay-exporter");
    await expect(page.locator(".main")).toContainText("relay_response");
    await expect(page.locator(".main")).toContainText("relay_target_response");
    await expect(page.locator(".main")).toContainText("relay_target_http_status");
    await expect(page.locator(".main")).toContainText("# HELP relay_response");
    await expect(page.locator(".main")).toContainText("# TYPE relay_response gauge");
  });

  test("relay exporter page has RFC 1918 documentation", async ({ page }) => {
    await page.goto("./#/relay-exporter");
    await expect(page.locator(".main")).toContainText("RFC 1918");
    await expect(page.locator(".main")).toContainText("10.0.0.0/8");
    await expect(page.locator(".main")).toContainText("172.16.0.0/12");
    await expect(page.locator(".main")).toContainText("192.168.0.0/16");
  });

  test("relay exporter page has source IP filtering documentation", async ({ page }) => {
    await page.goto("./#/relay-exporter");
    await expect(page.locator(".main")).toContainText("Source IP Filtering");
    await expect(page.locator(".main")).toContainText("403 Forbidden");
    await expect(page.locator(".main")).toContainText("refuses to start");
  });

  test("relay exporter page has HTTP status codes table", async ({ page }) => {
    await page.goto("./#/relay-exporter");
    await expect(page.locator(".main")).toContainText("400");
    await expect(page.locator(".main")).toContainText("403");
    await expect(page.locator(".main")).toContainText("429");
  });

  test("relay exporter page has architecture section", async ({ page }) => {
    await page.goto("./#/relay-exporter");
    await expect(page.locator(".main")).toContainText("Architecture");
    await expect(page.locator(".main")).toContainText("relay_exporter");
    await expect(page.locator(".main")).toContainText("Request Flow");
  });

  test("relay exporter page has TLS section", async ({ page }) => {
    await page.goto("./#/relay-exporter");
    await expect(page.locator(".main")).toContainText("Relay Listener TLS");
    await expect(page.locator(".main")).toContainText("Target Connection TLS");
    await expect(page.locator(".main")).toContainText("tls=true");
  });

  test("relay exporter page has Prometheus configuration", async ({ page }) => {
    await page.goto("./#/relay-exporter");
    await expect(page.locator(".main")).toContainText("relabel_configs");
    await expect(page.locator(".main")).toContainText("__param_ip");
    await expect(page.locator(".main")).toContainText("__param_port");
    await expect(page.locator(".main")).toContainText("multi-target");
  });

  test("relay exporter page has endpoints section", async ({ page }) => {
    await page.goto("./#/relay-exporter");
    await expect(page.locator(".main")).toContainText("/metrics");
    await expect(page.locator(".main")).toContainText("/health");
  });

  test("relay exporter page has deployment section", async ({ page }) => {
    await page.goto("./#/relay-exporter");
    await expect(page.locator(".main")).toContainText("Kubernetes");
    await expect(page.locator(".main")).toContainText("Docker Compose");
    await expect(page.locator(".main")).toContainText("Deployment");
  });

  test("relay exporter page has failure modes section", async ({ page }) => {
    await page.goto("./#/relay-exporter");
    await expect(page.locator(".main")).toContainText("Failure Modes");
    await expect(page.locator(".main")).toContainText("Target unreachable");
    await expect(page.locator(".main")).toContainText("Proxy timeout");
    await expect(page.locator(".main")).toContainText("Concurrent request limit");
  });

  test("relay exporter page has security section", async ({ page }) => {
    await page.goto("./#/relay-exporter");
    await expect(page.locator(".main")).toContainText("Security");
    await expect(page.locator(".main")).toContainText("No open proxy");
    await expect(page.locator(".main")).toContainText("Authorization forwarding");
  });

  test("home page lists relay_exporter and port 9100", async ({ page }) => {
    await page.goto("./");
    await expect(page.locator("body")).toContainText("relay_exporter");
    await expect(page.locator("body")).toContainText("9100");
  });

  test("navigation to relay exporter page works", async ({ page }) => {
    await page.goto("./");
    await page.click('a[href="#/relay-exporter"]');
    await expect(page.locator(".main")).toContainText("Relay Exporter");
  });
});
