export function HomePage(): string {
  const exporters = [
    ["network_exporter", "Network connectivity and performance metrics", "9101"],
    ["ipsec_exporter", "IPsec tunnel status and traffic metrics", "9102"],
    ["cloudflare_exporter", "Cloudflare analytics, Zero Trust, DNS, and certificate metrics", "9199"],
    ["libvirt_exporter", "Libvirt/KVM virtual machine and hypervisor metrics", "9177"],
    ["openbao_exporter", "OpenBao cluster health, raft, and native metrics", "9100"],
    ["relay_exporter", "Prometheus metrics relay proxy for RFC 1918 targets", "9100"],
  ];

  const exporterRows = exporters
    .map(([name, desc, port]) => `<tr><td><strong>${name}</strong></td><td>${desc}</td><td>${port}</td></tr>`)
    .join("");

  return `
    <div class="hero">
      <h1>Prometheus Exporters</h1>
      <p>A collection of Prometheus exporter containers for monitoring network infrastructure, IPsec tunnels, and Cloudflare services.</p>
      <div class="hero-badges">
        <span class="badge badge-primary">Prometheus</span>
        <span class="badge">Go</span>
        <span class="badge">MIT License</span>
        <span class="badge">Container Images</span>
      </div>
    </div>
    <div class="section">
      <h2>Exporters</h2>
      <table>
        <thead><tr><th>Exporter</th><th>Description</th><th>Default Port</th></tr></thead>
        <tbody>${exporterRows}</tbody>
      </table>
    </div>
    <div class="section">
      <h2>Quick Start</h2>
      <pre><code># Pull an exporter image
docker pull ghcr.io/phaseshiftdata/cloudflare_exporter:main

# Run with an API token
docker run -d -p 9199:9199 \\
  -e CF_API_TOKEN="your-cloudflare-api-token" \\
  ghcr.io/phaseshiftdata/cloudflare_exporter:main</code></pre>
    </div>
    <div class="section">
      <h2>Container Images</h2>
      <p>Images are published to GitHub Container Registry (GHCR):</p>
      <pre><code>ghcr.io/phaseshiftdata/network_exporter
ghcr.io/phaseshiftdata/ipsec_exporter
ghcr.io/phaseshiftdata/cloudflare_exporter</code></pre>
      <table>
        <thead><tr><th>Trigger</th><th>Tag</th></tr></thead>
        <tbody>
          <tr><td>Push to feature branch / PR</td><td><code>:&lt;commit-sha&gt;</code></td></tr>
          <tr><td>Push to <code>main</code></td><td><code>:main</code></td></tr>
          <tr><td>Git tag (e.g. <code>v1.0.0</code>)</td><td><code>:&lt;git-tag&gt;</code></td></tr>
        </tbody>
      </table>
    </div>
    <div class="section">
      <h2>Development</h2>
      <pre><code># Clone the repository
git clone https://github.com/phaseshiftdata/prometheus_exporters.git
cd prometheus_exporters

# Build all exporter images
make build

# Run linters
make lint

# Run all tests
make test

# Push images to GHCR
make deploy</code></pre>
    </div>`;
}
