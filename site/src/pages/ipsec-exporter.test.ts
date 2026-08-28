import { describe, it, expect } from "vitest";
import { IpsecExporterPage } from "./ipsec-exporter.js";

describe("IpsecExporterPage", () => {
  it("returns HTML containing the page title", () => {
    const html = IpsecExporterPage();
    expect(html).toContain("IPsec Exporter");
  });

  it("contains VICI socket documentation", () => {
    const html = IpsecExporterPage();
    expect(html).toContain("VICI");
    expect(html).toContain("/var/run/charon.vici");
    expect(html).toContain("strongSwan");
    expect(html).toContain("list-sas");
    expect(html).toContain("govici");
  });

  it("contains IPsec metrics", () => {
    const html = IpsecExporterPage();
    expect(html).toContain("ipsec_up");
    expect(html).toContain("ipsec_ike_sa_state");
    expect(html).toContain("ipsec_child_sa_bytes_in");
    expect(html).toContain("ipsec_child_sa_bytes_out");
    expect(html).toContain("ipsec_uptime_seconds");
    expect(html).toContain("ipsec_workers_total");
    expect(html).toContain("ipsec_queues");
  });

  it("contains configuration flags", () => {
    const html = IpsecExporterPage();
    expect(html).toContain("--listen-address");
    expect(html).toContain("--proc-path");
    expect(html).toContain("--sys-path");
    expect(html).toContain("--vici-socket");
    expect(html).toContain("--log-level");
  });

  it("contains SA state tables", () => {
    const html = IpsecExporterPage();
    expect(html).toContain("ESTABLISHED");
    expect(html).toContain("CONNECTING");
    expect(html).toContain("INSTALLED");
    expect(html).toContain("REKEYED");
  });

  it("contains network metrics section", () => {
    const html = IpsecExporterPage();
    expect(html).toContain("network_arp_entry");
    expect(html).toContain("network_firewall_drop_packets_total");
    expect(html).toContain("network_port_connections");
  });

  it("contains tunnel auto-discovery section", () => {
    const html = IpsecExporterPage();
    expect(html).toContain("Tunnel Auto-Discovery");
    expect(html).toContain("uid");
  });

  it("contains failure modes section", () => {
    const html = IpsecExporterPage();
    expect(html).toContain("VICI socket unavailable");
    expect(html).toContain("ContinueOnError");
    expect(html).toContain("ipsec_up 0");
  });

  it("contains docker run requirements", () => {
    const html = IpsecExporterPage();
    expect(html).toContain("--network=host");
    expect(html).toContain("--pid=host");
    expect(html).toContain("--user 0:0");
    expect(html).toContain("--cap-add=NET_ADMIN");
    expect(html).toContain("--cap-add=DAC_READ_SEARCH");
    expect(html).toContain("--security-opt label=disable");
    expect(html).toContain("/host/proc");
    expect(html).toContain("/host/sys");
    expect(html).toContain("/var/run/charon.vici");
  });

  it("contains installation instructions", () => {
    const html = IpsecExporterPage();
    expect(html).toContain("ghcr.io/phaseshiftdata/ipsec_exporter");
  });

  it("renders 4-column metric tables with labels column", () => {
    const html = IpsecExporterPage();
    // IPsec metrics table has Label column (4-col)
    expect(html).toContain("<th>Metric</th><th>Type</th><th>Labels</th><th>Description</th>");
  });

  it("renders state tables with value, state, and description columns", () => {
    const html = IpsecExporterPage();
    expect(html).toContain("<th>Value</th><th>State</th><th>Description</th>");
  });

  it("renders metric rows with empty labels correctly", () => {
    const html = IpsecExporterPage();
    // ipsec_up has no labels (empty string)
    expect(html).toContain("<code>ipsec_up</code>");
  });
});
