import { describe, it, expect } from "vitest";
import { NetworkExporterPage } from "./network-exporter.js";

describe("NetworkExporterPage", () => {
  it("returns HTML containing the page title", () => {
    const html = NetworkExporterPage();
    expect(html).toContain("Network Exporter");
  });

  it("contains configuration flags", () => {
    const html = NetworkExporterPage();
    expect(html).toContain("--listen-address");
    expect(html).toContain("--proc-path");
    expect(html).toContain("--sys-path");
    expect(html).toContain("--log-level");
  });

  it("contains ARP metrics", () => {
    const html = NetworkExporterPage();
    expect(html).toContain("network_arp_entry");
    expect(html).toContain("Metrics: ARP");
  });

  it("contains interface metrics", () => {
    const html = NetworkExporterPage();
    expect(html).toContain("network_interface_type");
    expect(html).toContain("network_bond_member");
    expect(html).toContain("network_bridge_member");
  });

  it("contains network graph metrics", () => {
    const html = NetworkExporterPage();
    expect(html).toContain("network_graph_edge");
    expect(html).toContain("Metrics: Network Graph");
  });

  it("contains conntrack metrics", () => {
    const html = NetworkExporterPage();
    expect(html).toContain("network_port_connections");
    expect(html).toContain("network_conntrack_accounting_enabled");
  });

  it("contains firewall metrics", () => {
    const html = NetworkExporterPage();
    expect(html).toContain("network_firewall_collector_up");
    expect(html).toContain("network_firewall_drop_packets_total");
  });

  it("contains architecture section", () => {
    const html = NetworkExporterPage();
    expect(html).toContain("Architecture");
    expect(html).toContain("NETLINK_ROUTE");
    expect(html).toContain("NETLINK_NETFILTER");
    expect(html).toContain("procfs");
    expect(html).toContain("sysfs");
  });

  it("contains installation and docker requirements", () => {
    const html = NetworkExporterPage();
    expect(html).toContain("ghcr.io/phaseshiftdata/network_exporter");
    expect(html).toContain("--network=host");
    expect(html).toContain("--pid=host");
    expect(html).toContain("--user 0:0");
    expect(html).toContain("--cap-add=NET_ADMIN");
    expect(html).toContain("--cap-add=DAC_READ_SEARCH");
    expect(html).toContain("--security-opt label=disable");
    expect(html).toContain("/host/proc");
    expect(html).toContain("/host/sys");
  });

  it("contains deployment section with DaemonSet", () => {
    const html = NetworkExporterPage();
    expect(html).toContain("Deployment");
    expect(html).toContain("DaemonSet");
    expect(html).toContain("CAP_NET_ADMIN");
    expect(html).toContain("hostPID");
    expect(html).toContain("hostNetwork");
  });

  it("contains failure modes section", () => {
    const html = NetworkExporterPage();
    expect(html).toContain("Failure Modes");
    expect(html).toContain("ContinueOnError");
    expect(html).toContain("nftables not available");
  });

  it("contains endpoints section", () => {
    const html = NetworkExporterPage();
    expect(html).toContain("/metrics");
  });
});
