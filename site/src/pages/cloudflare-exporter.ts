export function CloudflareExporterPage(): string {
  return `
    <div class="section">
      <h2>Cloudflare Exporter</h2>
      <p>Monitors Cloudflare zone and DNS analytics, exposing metrics for Prometheus scraping.</p>
      <pre><code>docker pull ghcr.io/asymmetric-effort/cloudflare_exporter:main
docker run -p 9103:9103 ghcr.io/asymmetric-effort/cloudflare_exporter:main</code></pre>
    </div>
    <div class="section">
      <h2>Configuration</h2>
      <p>Configuration details will be documented as the exporter is developed.</p>
    </div>
    <div class="section">
      <h2>Metrics</h2>
      <p>Metrics reference will be documented as the exporter is developed.</p>
    </div>`;
}
