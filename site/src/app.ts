import { HomePage } from "./pages/home.js";
import { NetworkExporterPage } from "./pages/network-exporter.js";
import { IpsecExporterPage } from "./pages/ipsec-exporter.js";
import { CloudflareExporterPage } from "./pages/cloudflare-exporter.js";

declare const __APP_VERSION__: string;
const VERSION = typeof __APP_VERSION__ !== "undefined" ? __APP_VERSION__ : "0.0.0";

type PageComponent = () => string;

const ROUTES: Record<string, PageComponent> = Object.create(null);
ROUTES["/"] = HomePage;
ROUTES["/network-exporter"] = NetworkExporterPage;
ROUTES["/ipsec-exporter"] = IpsecExporterPage;
ROUTES["/cloudflare-exporter"] = CloudflareExporterPage;

function getPath(): string {
  const hash = window.location.hash.replace(/^#\/?/, "/");
  return hash === "" ? "/" : hash;
}

function renderNav(currentPath: string): string {
  const links = [
    { to: "/", label: "Home", exact: true },
    { to: "/network-exporter", label: "Network" },
    { to: "/ipsec-exporter", label: "IPsec" },
    { to: "/cloudflare-exporter", label: "Cloudflare" },
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

function renderFooter(): string {
  return `<footer class="footer" role="contentinfo">
    <div class="footer-inner">
      <span>v${VERSION}</span>
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

function render(): void {
  const path = getPath();
  const root = document.getElementById("root")!;
  const page = path in ROUTES ? ROUTES[path] : ROUTES["/"];

  root.innerHTML = `
    ${renderNav(path)}
    <main class="main">${page()}</main>
    ${renderFooter()}
  `;

  updateHead(path);
}

function updateHead(path: string): void {
  const titles: Record<string, string> = Object.create(null);
  titles["/"] = "Prometheus Exporters \u2014 Network, IPsec & Cloudflare";
  titles["/network-exporter"] = "Network Exporter \u2014 Prometheus Exporters";
  titles["/ipsec-exporter"] = "IPsec Exporter \u2014 Prometheus Exporters";
  titles["/cloudflare-exporter"] = "Cloudflare Exporter \u2014 Prometheus Exporters";

  document.title = path in titles ? titles[path] : titles["/"];

  let canonical = document.querySelector('link[rel="canonical"]') as HTMLLinkElement;
  if (!canonical) {
    canonical = document.createElement("link");
    canonical.rel = "canonical";
    document.head.appendChild(canonical);
  }
  canonical.href = `https://prometheus_exporters.phaseshiftdata.com/${path === "/" ? "" : "#" + path}`;
}

render();
window.addEventListener("hashchange", render);
