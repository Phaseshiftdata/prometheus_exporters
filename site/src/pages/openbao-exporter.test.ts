import { describe, it, expect } from "vitest";
import { OpenBaoExporterPage } from "./openbao-exporter.js";

describe("OpenBaoExporterPage", () => {
  it("returns HTML containing the page title", () => {
    const html = OpenBaoExporterPage();
    expect(html).toContain("OpenBao Exporter");
  });

  it("contains configuration flags", () => {
    const html = OpenBaoExporterPage();
    expect(html).toContain("--listen-address");
    expect(html).toContain("--openbao-addr");
    expect(html).toContain("--openbao-token");
    expect(html).toContain("--openbao-token-file");
    expect(html).toContain("--log-level");
    expect(html).toContain("--poll-interval");
  });

  it("contains health metrics documentation", () => {
    const html = OpenBaoExporterPage();
    expect(html).toContain("openbao_up");
    expect(html).toContain("openbao_initialized");
    expect(html).toContain("openbao_sealed");
    expect(html).toContain("openbao_standby");
    expect(html).toContain("openbao_leader");
    expect(html).toContain("openbao_node_info");
  });

  it("contains cluster metrics documentation", () => {
    const html = OpenBaoExporterPage();
    expect(html).toContain("openbao_raft_committed_index");
    expect(html).toContain("openbao_raft_applied_index");
    expect(html).toContain("openbao_peers");
  });

  it("contains architecture section", () => {
    const html = OpenBaoExporterPage();
    expect(html).toContain("Architecture");
    expect(html).toContain("Collection Flow");
  });

  it("contains health endpoint status codes", () => {
    const html = OpenBaoExporterPage();
    expect(html).toContain("200");
    expect(html).toContain("429");
    expect(html).toContain("472");
    expect(html).toContain("501");
    expect(html).toContain("503");
  });

  it("contains cluster discovery section", () => {
    const html = OpenBaoExporterPage();
    expect(html).toContain("Cluster Discovery");
    expect(html).toContain("/v1/sys/storage/raft/configuration");
    expect(html).toContain("openbao_peers 1");
  });

  it("contains endpoints section", () => {
    const html = OpenBaoExporterPage();
    expect(html).toContain("/metrics");
  });

  it("contains deployment section", () => {
    const html = OpenBaoExporterPage();
    expect(html).toContain("Kubernetes");
    expect(html).toContain("Docker Compose");
  });

  it("contains failure modes section", () => {
    const html = OpenBaoExporterPage();
    expect(html).toContain("Failure Modes");
    expect(html).toContain("Seed node unreachable");
    expect(html).toContain("Authentication failure");
    expect(html).toContain("Raft not available");
  });

  it("contains security section", () => {
    const html = OpenBaoExporterPage();
    expect(html).toContain("Security");
    expect(html).toContain("Token handling");
    expect(html).toContain("Distroless container");
    expect(html).toContain("Response body limit");
  });

  it("contains installation instructions", () => {
    const html = OpenBaoExporterPage();
    expect(html).toContain("ghcr.io/phaseshiftdata/openbao_exporter");
  });

  it("contains native metrics section", () => {
    const html = OpenBaoExporterPage();
    expect(html).toContain("Native Metrics");
    expect(html).toContain("/v1/sys/metrics?format=prometheus");
  });
});
