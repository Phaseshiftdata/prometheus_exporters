import { describe, it, expect } from "vitest";
import { RelayExporterPage } from "./relay-exporter.js";

describe("RelayExporterPage", () => {
  it("returns HTML containing the page title", () => {
    const html = RelayExporterPage();
    expect(html).toContain("Relay Exporter");
  });

  it("contains configuration flags", () => {
    const html = RelayExporterPage();
    expect(html).toContain("--listen-address");
    expect(html).toContain("--allowed-source");
    expect(html).toContain("--tls-cert-file");
    expect(html).toContain("--tls-key-file");
    expect(html).toContain("--ca-cert");
    expect(html).toContain("--tls-skip-verify");
    expect(html).toContain("--proxy-timeout");
    expect(html).toContain("--concurrent-requests");
    expect(html).toContain("--log-level");
  });

  it("contains response format documentation", () => {
    const html = RelayExporterPage();
    expect(html).toContain("relay_response");
    expect(html).toContain("relay_target_response");
    expect(html).toContain("relay_target_http_status");
    expect(html).toContain("# HELP relay_response");
    expect(html).toContain("# TYPE relay_response gauge");
  });

  it("contains RFC 1918 documentation", () => {
    const html = RelayExporterPage();
    expect(html).toContain("RFC 1918");
    expect(html).toContain("10.0.0.0/8");
    expect(html).toContain("172.16.0.0/12");
    expect(html).toContain("192.168.0.0/16");
  });

  it("contains source IP filtering documentation", () => {
    const html = RelayExporterPage();
    expect(html).toContain("Source IP Filtering");
    expect(html).toContain("403 Forbidden");
    expect(html).toContain("refuses to start");
  });

  it("contains HTTP status codes", () => {
    const html = RelayExporterPage();
    expect(html).toContain("400");
    expect(html).toContain("403");
    expect(html).toContain("429");
  });

  it("contains architecture section", () => {
    const html = RelayExporterPage();
    expect(html).toContain("Architecture");
    expect(html).toContain("Request Flow");
  });

  it("contains TLS section", () => {
    const html = RelayExporterPage();
    expect(html).toContain("Relay Listener TLS");
    expect(html).toContain("Target Connection TLS");
    expect(html).toContain("tls=true");
  });

  it("contains Prometheus configuration", () => {
    const html = RelayExporterPage();
    expect(html).toContain("relabel_configs");
    expect(html).toContain("__param_ip");
    expect(html).toContain("__param_port");
    expect(html).toContain("multi-target");
  });

  it("contains endpoints section", () => {
    const html = RelayExporterPage();
    expect(html).toContain("/metrics");
    expect(html).toContain("/health");
  });

  it("contains deployment section", () => {
    const html = RelayExporterPage();
    expect(html).toContain("Kubernetes");
    expect(html).toContain("Docker Compose");
  });

  it("contains failure modes section", () => {
    const html = RelayExporterPage();
    expect(html).toContain("Failure Modes");
    expect(html).toContain("Target unreachable");
    expect(html).toContain("Proxy timeout");
    expect(html).toContain("Concurrent request limit");
  });

  it("contains security section", () => {
    const html = RelayExporterPage();
    expect(html).toContain("Security");
    expect(html).toContain("No open proxy");
    expect(html).toContain("Authorization forwarding");
  });

  it("contains installation instructions", () => {
    const html = RelayExporterPage();
    expect(html).toContain("ghcr.io/phaseshiftdata/relay_exporter");
  });
});
