import { describe, it, expect } from "vitest";
import { LibvirtExporterPage } from "./libvirt-exporter.js";

describe("LibvirtExporterPage", () => {
  it("returns HTML containing the page title", () => {
    const html = LibvirtExporterPage();
    expect(html).toContain("Libvirt Exporter");
  });

  it("contains configuration flags", () => {
    const html = LibvirtExporterPage();
    expect(html).toContain("--libvirt-uri");
    expect(html).toContain("--listen-address");
    expect(html).toContain("--log-level");
  });

  it("contains hypervisor metrics", () => {
    const html = LibvirtExporterPage();
    expect(html).toContain("libvirt_up");
    expect(html).toContain("libvirt_domains_total");
    expect(html).toContain("libvirt_host_cpu_count");
    expect(html).toContain("libvirt_host_memory_bytes");
  });

  it("contains domain info metrics", () => {
    const html = LibvirtExporterPage();
    expect(html).toContain("libvirt_domain_info_state");
    expect(html).toContain("libvirt_domain_cpu_time_seconds_total");
    expect(html).toContain("libvirt_domain_info_vcpus");
  });

  it("contains block device metrics", () => {
    const html = LibvirtExporterPage();
    expect(html).toContain("libvirt_domain_block_read_bytes_total");
    expect(html).toContain("libvirt_domain_block_write_bytes_total");
  });

  it("contains network interface metrics", () => {
    const html = LibvirtExporterPage();
    expect(html).toContain("libvirt_domain_net_receive_bytes_total");
    expect(html).toContain("libvirt_domain_net_transmit_bytes_total");
  });

  it("contains domain states table", () => {
    const html = LibvirtExporterPage();
    expect(html).toContain("RUNNING");
    expect(html).toContain("PAUSED");
    expect(html).toContain("SHUTOFF");
    expect(html).toContain("CRASHED");
  });

  it("contains architecture section", () => {
    const html = LibvirtExporterPage();
    expect(html).toContain("Architecture");
    expect(html).toContain("ContinueOnError");
  });

  it("contains installation instructions", () => {
    const html = LibvirtExporterPage();
    expect(html).toContain("ghcr.io/phaseshiftdata/libvirt_exporter");
  });

  it("contains CGO note", () => {
    const html = LibvirtExporterPage();
    expect(html).toContain("CGO");
    expect(html).toContain("libvirt-dev");
  });

  it("contains deployment section", () => {
    const html = LibvirtExporterPage();
    expect(html).toContain("DaemonSet");
    expect(html).toContain("Docker Compose");
  });

  it("contains failure modes section", () => {
    const html = LibvirtExporterPage();
    expect(html).toContain("Failure Modes");
    expect(html).toContain("libvirtd unreachable");
    expect(html).toContain("libvirt_up 0");
  });
});
