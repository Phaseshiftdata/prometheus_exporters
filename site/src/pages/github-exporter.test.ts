import { describe, it, expect } from "vitest";
import { GitHubExporterPage } from "./github-exporter.js";

describe("GitHubExporterPage", () => {
  it("returns HTML containing the page title", () => {
    const html = GitHubExporterPage();
    expect(html).toContain("GitHub Exporter");
  });

  it("contains configuration flags", () => {
    const html = GitHubExporterPage();
    expect(html).toContain("--github-app-id");
    expect(html).toContain("--poll-interval");
    expect(html).toContain("--listen-address");
    expect(html).toContain("--database-url");
  });

  it("contains CI/CD metrics", () => {
    const html = GitHubExporterPage();
    expect(html).toContain("github_exporter_workflow_runs_total");
    expect(html).toContain("github_exporter_open_pull_requests");
    expect(html).toContain("github_exporter_commits_total");
  });

  it("contains rate limit metrics", () => {
    const html = GitHubExporterPage();
    expect(html).toContain("github_exporter_rate_limit_remaining");
    expect(html).toContain("github_exporter_rate_limited_total");
  });

  it("contains backfill metrics", () => {
    const html = GitHubExporterPage();
    expect(html).toContain("github_exporter_backfill_pending_job_runs");
    expect(html).toContain("github_exporter_backfill_last_step_timestamp_seconds");
    expect(html).toContain("github_exporter_api_requests_total");
  });

  it("contains architecture section", () => {
    const html = GitHubExporterPage();
    expect(html).toContain("Poller");
    expect(html).toContain("Backfiller");
    expect(html).toContain("Rate Limit Manager");
    expect(html).toContain("PostgreSQL Store");
  });

  it("contains GitHub App setup section", () => {
    const html = GitHubExporterPage();
    expect(html).toContain("GitHub App Setup");
    expect(html).toContain("GitHub App installation");
    expect(html).toContain("5,000 requests per hour");
  });

  it("contains database section", () => {
    const html = GitHubExporterPage();
    expect(html).toContain("Database");
    expect(html).toContain("PostgreSQL 14");
    expect(html).toContain("90 days");
  });

  it("contains rate limiting section", () => {
    const html = GitHubExporterPage();
    expect(html).toContain("Rate Limiting");
    expect(html).toContain("Primary");
    expect(html).toContain("Secondary");
  });

  it("contains secret configuration section", () => {
    const html = GitHubExporterPage();
    expect(html).toContain("Secret Configuration");
    expect(html).toContain("database-password-file");
    expect(html).toContain("openbao");
  });

  it("contains failure modes section", () => {
    const html = GitHubExporterPage();
    expect(html).toContain("Failure Modes");
    expect(html).toContain("Secondary rate limit");
    expect(html).toContain("Database unavailable");
  });

  it("contains installation instructions", () => {
    const html = GitHubExporterPage();
    expect(html).toContain("ghcr.io/phaseshiftdata/github_exporter");
    expect(html).toContain("docker");
  });

  it("contains endpoints section", () => {
    const html = GitHubExporterPage();
    expect(html).toContain("/metrics");
  });
});
