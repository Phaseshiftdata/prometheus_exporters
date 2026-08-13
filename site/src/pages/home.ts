export function HomePage(): string {
  const exporters = [
    ["network_exporter", "Network connectivity and performance metrics", "9101"],
    ["ipsec_exporter", "IPsec tunnel status and traffic metrics", "9102"],
    ["cloudflare_exporter", "Cloudflare zone and DNS analytics metrics", "9103"],
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
docker pull ghcr.io/asymmetric-effort/network_exporter:main

# Run with default configuration
docker run -p 9101:9101 ghcr.io/asymmetric-effort/network_exporter:main</code></pre>
    </div>
    <div class="section">
      <h2>Container Image Tags</h2>
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
git clone https://github.com/asymmetric-effort/prometheus_exporters.git
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
