import { describe, it, expect } from "vitest";
import { HomePage } from "./home.js";

describe("HomePage", () => {
  it("returns a string containing the page title", () => {
    const html = HomePage();
    expect(html).toContain("Prometheus Exporters");
  });

  it("lists all five exporters", () => {
    const html = HomePage();
    expect(html).toContain("network_exporter");
    expect(html).toContain("ipsec_exporter");
    expect(html).toContain("cloudflare_exporter");
    expect(html).toContain("libvirt_exporter");
    expect(html).toContain("relay_exporter");
  });

  it("includes default ports for each exporter", () => {
    const html = HomePage();
    expect(html).toContain("9101");
    expect(html).toContain("9102");
    expect(html).toContain("9199");
    expect(html).toContain("9177");
    expect(html).toContain("9100");
  });

  it("contains hero section with badges", () => {
    const html = HomePage();
    expect(html).toContain("hero");
    expect(html).toContain("hero-badges");
    expect(html).toContain("Prometheus");
    expect(html).toContain("MIT License");
  });

  it("contains quick start section with docker commands", () => {
    const html = HomePage();
    expect(html).toContain("Quick Start");
    expect(html).toContain("docker pull");
    expect(html).toContain("ghcr.io/phaseshiftdata/cloudflare_exporter:main");
  });

  it("contains container images section with tagging policy", () => {
    const html = HomePage();
    expect(html).toContain("Container Images");
    expect(html).toContain("ghcr.io/phaseshiftdata");
    expect(html).toContain(":main");
    expect(html).toContain("commit-sha");
  });

  it("contains development section", () => {
    const html = HomePage();
    expect(html).toContain("Development");
    expect(html).toContain("make build");
    expect(html).toContain("make test");
  });

  it("renders table rows for each exporter", () => {
    const html = HomePage();
    expect(html).toContain("<tr><td><strong>network_exporter</strong></td>");
    expect(html).toContain("<tr><td><strong>cloudflare_exporter</strong></td>");
  });
});
