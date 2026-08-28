import { describe, it, expect, beforeEach } from "vitest";

/**
 * Tests for the app module's routing, navigation, footer, and head-management logic.
 *
 * We must set up a #root element before importing app.ts because it calls render()
 * at the top level. The import is at module scope so the jsdom environment must
 * already have the element when the test file is evaluated.
 */

// Prepare DOM before app.ts is imported (top-level side effects need #root).
document.body.innerHTML = '<div id="root"></div>';

import {
  VERSION,
  ROUTES,
  getPath,
  renderNav,
  renderFooter,
  render,
  updateHead,
} from "./app.js";

/* ------------------------------------------------------------------ */
/*  VERSION                                                            */
/* ------------------------------------------------------------------ */

describe("VERSION", () => {
  it("is a non-empty semver string", () => {
    expect(VERSION).toMatch(/^\d+\.\d+\.\d+/);
  });
});

/* ------------------------------------------------------------------ */
/*  ROUTES                                                             */
/* ------------------------------------------------------------------ */

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

/* ------------------------------------------------------------------ */
/*  getPath                                                            */
/* ------------------------------------------------------------------ */

describe("getPath", () => {
  it("returns / when hash is empty", () => {
    window.location.hash = "";
    expect(getPath()).toBe("/");
  });

  it("returns / for #/", () => {
    window.location.hash = "#/";
    expect(getPath()).toBe("/");
  });

  it("returns /network-exporter for #/network-exporter", () => {
    window.location.hash = "#/network-exporter";
    expect(getPath()).toBe("/network-exporter");
  });

  it("handles hash without leading slash", () => {
    window.location.hash = "#cloudflare-exporter";
    expect(getPath()).toBe("/cloudflare-exporter");
  });

  it("returns / for # alone", () => {
    window.location.hash = "#";
    expect(getPath()).toBe("/");
  });
});

/* ------------------------------------------------------------------ */
/*  renderNav                                                          */
/* ------------------------------------------------------------------ */

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

  it("does not mark home active for sub-routes", () => {
    const nav = renderNav("/ipsec-exporter");
    expect(nav).not.toContain('class="active">Home</a>');
  });
});

/* ------------------------------------------------------------------ */
/*  renderFooter                                                       */
/* ------------------------------------------------------------------ */

describe("renderFooter", () => {
  it("includes version", () => {
    const footer = renderFooter();
    expect(footer).toContain(`v${VERSION}`);
  });

  it("includes MIT license", () => {
    const footer = renderFooter();
    expect(footer).toContain("MIT License");
    expect(footer).toContain("Asymmetric Effort");
  });

  it("includes GitHub, Security, and Contributing links", () => {
    const footer = renderFooter();
    expect(footer).toContain("github.com/phaseshiftdata/prometheus_exporters");
    expect(footer).toContain("SECURITY.md");
    expect(footer).toContain("CONTRIBUTING.md");
  });

  it("has correct footer role", () => {
    const footer = renderFooter();
    expect(footer).toContain('role="contentinfo"');
  });

  it("uses noopener noreferrer on external links", () => {
    const footer = renderFooter();
    expect(footer).toContain('rel="noopener noreferrer"');
  });
});

/* ------------------------------------------------------------------ */
/*  render                                                             */
/* ------------------------------------------------------------------ */

describe("render", () => {
  beforeEach(() => {
    document.body.innerHTML = '<div id="root"></div>';
    document.querySelectorAll('link[rel="canonical"]').forEach((el) => el.remove());
  });

  it("renders home page for empty hash", () => {
    window.location.hash = "";
    render();
    const root = document.getElementById("root")!;
    expect(root.innerHTML).toContain("Prometheus Exporters");
    expect(root.innerHTML).toContain("nav");
    expect(root.innerHTML).toContain("footer");
  });

  it("renders cloudflare page for #/cloudflare-exporter", () => {
    window.location.hash = "#/cloudflare-exporter";
    render();
    const root = document.getElementById("root")!;
    expect(root.innerHTML).toContain("Cloudflare Exporter");
  });

  it("renders each route successfully", () => {
    for (const route of Object.keys(ROUTES)) {
      window.location.hash = `#${route}`;
      render();
      const root = document.getElementById("root")!;
      expect(root.innerHTML.length).toBeGreaterThan(0);
    }
  });

  it("falls back to home for unknown routes", () => {
    window.location.hash = "#/nonexistent";
    render();
    const root = document.getElementById("root")!;
    expect(root.innerHTML).toContain("Prometheus Exporters");
  });

  it("includes nav, main, and footer sections", () => {
    window.location.hash = "#/";
    render();
    const root = document.getElementById("root")!;
    expect(root.querySelector("nav")).not.toBeNull();
    expect(root.querySelector("main")).not.toBeNull();
    expect(root.querySelector("footer")).not.toBeNull();
  });

  it("sets document title when rendering", () => {
    window.location.hash = "#/github-exporter";
    render();
    expect(document.title).toContain("GitHub Exporter");
  });
});

/* ------------------------------------------------------------------ */
/*  updateHead                                                         */
/* ------------------------------------------------------------------ */

describe("updateHead", () => {
  beforeEach(() => {
    document.querySelectorAll('link[rel="canonical"]').forEach((el) => el.remove());
  });

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

  it("sets title for ipsec exporter", () => {
    updateHead("/ipsec-exporter");
    expect(document.title).toContain("IPsec Exporter");
  });

  it("sets title for libvirt exporter", () => {
    updateHead("/libvirt-exporter");
    expect(document.title).toContain("Libvirt Exporter");
  });

  it("sets title for relay exporter", () => {
    updateHead("/relay-exporter");
    expect(document.title).toContain("Relay Exporter");
  });
});
