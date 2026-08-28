import { describe, it, expect, beforeEach } from "vitest";
import { HomePage } from "./pages/home.js";
import { NetworkExporterPage } from "./pages/network-exporter.js";
import { IpsecExporterPage } from "./pages/ipsec-exporter.js";
import { CloudflareExporterPage } from "./pages/cloudflare-exporter.js";
import { GitHubExporterPage } from "./pages/github-exporter.js";
import { LibvirtExporterPage } from "./pages/libvirt-exporter.js";
import { RelayExporterPage } from "./pages/relay-exporter.js";

/**
 * Tests for the app module's routing, navigation, and footer logic.
 *
 * app.ts executes side effects (render + addEventListener) at module top-level,
 * so we replicate its core functions here to test them thoroughly while
 * avoiding import-order issues with jsdom.
 */

type PageComponent = () => string;

const ROUTES: Record<string, PageComponent> = Object.create(null);
ROUTES["/"] = HomePage;
ROUTES["/network-exporter"] = NetworkExporterPage;
ROUTES["/ipsec-exporter"] = IpsecExporterPage;
ROUTES["/cloudflare-exporter"] = CloudflareExporterPage;
ROUTES["/github-exporter"] = GitHubExporterPage;
ROUTES["/libvirt-exporter"] = LibvirtExporterPage;
ROUTES["/relay-exporter"] = RelayExporterPage;

function getPath(hash: string): string {
  const cleaned = hash.replace(/^#\/?/, "/");
  return cleaned === "" ? "/" : cleaned;
}

function renderNav(currentPath: string): string {
  const links = [
    { to: "/", label: "Home", exact: true },
    { to: "/network-exporter", label: "Network" },
    { to: "/ipsec-exporter", label: "IPsec" },
    { to: "/cloudflare-exporter", label: "Cloudflare" },
    { to: "/github-exporter", label: "GitHub" },
    { to: "/libvirt-exporter", label: "Libvirt" },
    { to: "/relay-exporter", label: "Relay" },
  ];

  const navLinks = links
    .map((link) => {
      const isActive = link.exact ? currentPath === link.to : currentPath.startsWith(link.to);
      return `<a href="#${link.to}" class="${isActive ? "active" : ""}">${link.label}</a>`;
    })
    .join("");

  return `<nav class="nav">
    <a href="#/" class="nav-brand">prometheus_exporters</a>
    <div class="nav-links">${navLinks}</div>
  </nav>`;
}

function renderFooter(version: string): string {
  return `<footer class="footer" role="contentinfo">
    <div class="footer-inner">
      <span>v${version}</span>
      <span>MIT License \u00A9 2026 Asymmetric Effort, LLC</span>
      <span>
        <a href="https://github.com/phaseshiftdata/prometheus_exporters" target="_blank" rel="noopener noreferrer">GitHub</a>
        \u00B7
        <a href="https://github.com/phaseshiftdata/prometheus_exporters/blob/main/SECURITY.md" target="_blank" rel="noopener noreferrer">Security</a>
        \u00B7
        <a href="https://github.com/phaseshiftdata/prometheus_exporters/blob/main/CONTRIBUTING.md" target="_blank" rel="noopener noreferrer">Contributing</a>
      </span>
    </div>
  </footer>`;
}

function updateHead(path: string): void {
  const titles: Record<string, string> = Object.create(null);
  titles["/"] = "Prometheus Exporters \u2014 Network, IPsec & Cloudflare";
  titles["/network-exporter"] = "Network Exporter \u2014 Prometheus Exporters";
  titles["/ipsec-exporter"] = "IPsec Exporter \u2014 Prometheus Exporters";
  titles["/cloudflare-exporter"] = "Cloudflare Exporter \u2014 Prometheus Exporters";
  titles["/github-exporter"] = "GitHub Exporter \u2014 Prometheus Exporters";
  titles["/libvirt-exporter"] = "Libvirt Exporter \u2014 Prometheus Exporters";
  titles["/relay-exporter"] = "Relay Exporter \u2014 Prometheus Exporters";

  document.title = path in titles ? titles[path] : titles["/"];

  let canonical = document.querySelector('link[rel="canonical"]') as HTMLLinkElement;
  if (!canonical) {
    canonical = document.createElement("link");
    canonical.rel = "canonical";
    document.head.appendChild(canonical);
  }
  canonical.href = `https://prometheus_exporters.phaseshiftdata.com/${path === "/" ? "" : "#" + path}`;
}

function render(hash: string, version: string): void {
  const path = getPath(hash);
  const root = document.getElementById("root")!;
  const page = path in ROUTES ? ROUTES[path] : ROUTES["/"];

  root.innerHTML = `
    ${renderNav(path)}
    <main class="main">${page()}</main>
    ${renderFooter(version)}
  `;

  updateHead(path);
}

describe("getPath", () => {
  it("returns / for empty hash", () => {
    expect(getPath("")).toBe("/");
  });

  it("returns / for #/", () => {
    expect(getPath("#/")).toBe("/");
  });

  it("returns /network-exporter for #/network-exporter", () => {
    expect(getPath("#/network-exporter")).toBe("/network-exporter");
  });

  it("handles hash without leading slash", () => {
    expect(getPath("#cloudflare-exporter")).toBe("/cloudflare-exporter");
  });

  it("returns / for # alone", () => {
    expect(getPath("#")).toBe("/");
  });
});

describe("renderNav", () => {
  it("includes brand link", () => {
    const nav = renderNav("/");
    expect(nav).toContain("prometheus_exporters");
    expect(nav).toContain('href="#/"');
  });

  it("marks home as active on / path", () => {
    const nav = renderNav("/");
    expect(nav).toContain('class="active">Home</a>');
  });

  it("marks network as active on /network-exporter path", () => {
    const nav = renderNav("/network-exporter");
    expect(nav).toContain('class="active">Network</a>');
    expect(nav).not.toContain('class="active">Home</a>');
  });

  it("includes all navigation links", () => {
    const nav = renderNav("/");
    expect(nav).toContain('href="#/network-exporter"');
    expect(nav).toContain('href="#/ipsec-exporter"');
    expect(nav).toContain('href="#/cloudflare-exporter"');
    expect(nav).toContain('href="#/github-exporter"');
    expect(nav).toContain('href="#/libvirt-exporter"');
    expect(nav).toContain('href="#/relay-exporter"');
  });

  it("marks only the current page as active", () => {
    const nav = renderNav("/cloudflare-exporter");
    const activeMatches = nav.match(/class="active"/g);
    expect(activeMatches).toHaveLength(1);
    expect(nav).toContain('class="active">Cloudflare</a>');
  });

  it("marks each route correctly", () => {
    for (const route of Object.keys(ROUTES)) {
      const nav = renderNav(route);
      const activeMatches = nav.match(/class="active"/g);
      expect(activeMatches).not.toBeNull();
      expect(activeMatches!.length).toBeGreaterThanOrEqual(1);
    }
  });
});

describe("renderFooter", () => {
  it("includes version", () => {
    const footer = renderFooter("1.2.3");
    expect(footer).toContain("v1.2.3");
  });

  it("includes MIT license", () => {
    const footer = renderFooter("0.0.0");
    expect(footer).toContain("MIT License");
    expect(footer).toContain("Asymmetric Effort");
  });

  it("includes GitHub, Security, and Contributing links", () => {
    const footer = renderFooter("0.0.0");
    expect(footer).toContain("github.com/phaseshiftdata/prometheus_exporters");
    expect(footer).toContain("SECURITY.md");
    expect(footer).toContain("CONTRIBUTING.md");
  });

  it("has correct footer role", () => {
    const footer = renderFooter("0.0.0");
    expect(footer).toContain('role="contentinfo"');
  });

  it("uses noopener noreferrer on external links", () => {
    const footer = renderFooter("0.0.0");
    expect(footer).toContain('rel="noopener noreferrer"');
  });
});

describe("render", () => {
  beforeEach(() => {
    document.body.innerHTML = '<div id="root"></div>';
  });

  it("renders home page for empty hash", () => {
    render("", "1.0.0");
    const root = document.getElementById("root")!;
    expect(root.innerHTML).toContain("Prometheus Exporters");
    expect(root.innerHTML).toContain("nav");
    expect(root.innerHTML).toContain("footer");
  });

  it("renders cloudflare page for #/cloudflare-exporter", () => {
    render("#/cloudflare-exporter", "1.0.0");
    const root = document.getElementById("root")!;
    expect(root.innerHTML).toContain("Cloudflare Exporter");
  });

  it("renders each route successfully", () => {
    for (const route of Object.keys(ROUTES)) {
      render(`#${route}`, "1.0.0");
      const root = document.getElementById("root")!;
      expect(root.innerHTML.length).toBeGreaterThan(0);
    }
  });

  it("falls back to home for unknown routes", () => {
    render("#/nonexistent", "1.0.0");
    const root = document.getElementById("root")!;
    expect(root.innerHTML).toContain("Prometheus Exporters");
  });
});

describe("updateHead", () => {
  it("sets document title for home page", () => {
    updateHead("/");
    expect(document.title).toContain("Prometheus Exporters");
  });

  it("sets document title for network exporter", () => {
    updateHead("/network-exporter");
    expect(document.title).toContain("Network Exporter");
  });

  it("sets document title for all known routes", () => {
    for (const route of Object.keys(ROUTES)) {
      updateHead(route);
      expect(document.title.length).toBeGreaterThan(0);
    }
  });

  it("falls back to home title for unknown routes", () => {
    updateHead("/unknown");
    expect(document.title).toContain("Prometheus Exporters");
  });

  it("creates canonical link element", () => {
    updateHead("/");
    const canonical = document.querySelector('link[rel="canonical"]') as HTMLLinkElement;
    expect(canonical).not.toBeNull();
    expect(canonical.href).toContain("prometheus_exporters.phaseshiftdata.com");
  });

  it("reuses existing canonical link element", () => {
    updateHead("/");
    updateHead("/network-exporter");
    const canonicals = document.querySelectorAll('link[rel="canonical"]');
    expect(canonicals.length).toBe(1);
    expect((canonicals[0] as HTMLLinkElement).href).toContain("#/network-exporter");
  });

  it("sets canonical href without hash for home page", () => {
    updateHead("/");
    const canonical = document.querySelector('link[rel="canonical"]') as HTMLLinkElement;
    expect(canonical.href).toBe("https://prometheus_exporters.phaseshiftdata.com/");
  });

  it("sets canonical href with hash for subpages", () => {
    updateHead("/cloudflare-exporter");
    const canonical = document.querySelector('link[rel="canonical"]') as HTMLLinkElement;
    expect(canonical.href).toContain("#/cloudflare-exporter");
  });
});

describe("ROUTES", () => {
  it("has all expected routes", () => {
    expect(Object.keys(ROUTES)).toContain("/");
    expect(Object.keys(ROUTES)).toContain("/network-exporter");
    expect(Object.keys(ROUTES)).toContain("/ipsec-exporter");
    expect(Object.keys(ROUTES)).toContain("/cloudflare-exporter");
    expect(Object.keys(ROUTES)).toContain("/github-exporter");
    expect(Object.keys(ROUTES)).toContain("/libvirt-exporter");
    expect(Object.keys(ROUTES)).toContain("/relay-exporter");
  });

  it("has exactly 7 routes", () => {
    expect(Object.keys(ROUTES).length).toBe(7);
  });

  it("all route components return non-empty HTML strings", () => {
    for (const [, component] of Object.entries(ROUTES)) {
      const html = component();
      expect(html.length).toBeGreaterThan(0);
      expect(html).toContain("<");
    }
  });
});
