export function IpsecExporterPage(): string {
  return `
    <div class="section">
      <h2>IPsec Exporter</h2>
      <p>Monitors IPsec tunnel status and traffic, exposing metrics for Prometheus scraping.</p>
      <pre><code>docker pull ghcr.io/asymmetric-effort/ipsec_exporter:main
docker run -p 9102:9102 ghcr.io/asymmetric-effort/ipsec_exporter:main</code></pre>
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
