export function CloudflareExporterPage(): string {
  const configRows = [
    ["CF_API_TOKEN", "<em>(required)</em>", "Cloudflare API token with appropriate permissions."],
    ["CF_ACCOUNTS", "<em>(auto-discovered)</em>", "Comma-separated account IDs to monitor. When omitted, all accounts visible to the token are used."],
    ["CF_ZONES", "<em>(auto-discovered)</em>", "Comma-separated zone IDs to monitor. When omitted, all zones visible to the token are used."],
    ["CF_ZONES_EXCLUDE", "<em>(empty)</em>", "Comma-separated zone IDs to exclude from monitoring."],
    ["CF_SCRAPE_DELAY_SECONDS", "300", "Propagation delay (seconds) subtracted from \"now\" when querying analytics. Cloudflare's pipeline has roughly a five-minute lag."],
    ["CF_TIME_WINDOW_SECONDS", "60", "Width of each query window (seconds) for each collection cycle."],
    ["CF_REFRESH_INTERVAL_SECONDS", "60", "How often (seconds) the scheduler triggers a new collection cycle."],
    ["CF_DISCOVERY_INTERVAL_SECONDS", "21600", "How often (seconds) capability discovery re-runs to pick up new zones or product entitlements. Default is six hours."],
    ["CF_GRAPHQL_BUDGET_PER_WINDOW", "160", "Maximum GraphQL API calls allowed per five-minute sliding window."],
    ["CF_REST_BUDGET_PER_WINDOW", "600", "Maximum REST API calls allowed per five-minute sliding window."],
    ["CF_COLLECTORS_ENABLED", "<em>(all discovered)</em>", "Comma-separated list of collector names to enable. When omitted, all collectors whose capabilities are satisfied are enabled."],
    ["CF_GATEWAY_CATEGORY_ALLOWLIST", "<em>(empty)</em>", "Comma-separated allowlist of Gateway content categories to track."],
    ["CF_GATEWAY_CATEGORY_TOP_N", "25", "Maximum number of top Gateway categories to retain per cycle (limits label cardinality)."],
    ["CF_REQUEST_TIMEOUT_SECONDS", "10", "Per-request timeout (seconds) for Cloudflare API calls."],
    ["LISTEN_ADDRESS", ":9199", "Address and port the HTTP server listens on."],
    ["METRICS_PATH", "/metrics", "HTTP path where Prometheus metrics are served."],
    ["LOG_LEVEL", "info", "Log verbosity. One of debug, info, warn, error."],
  ];

  const configTable = configRows
    .map(([v, d, desc]) => `<tr><td><code>${v}</code></td><td><code>${d}</code></td><td>${desc}</td></tr>`)
    .join("");

  const ztMetrics = [
    ["access_login_requests_total", "counter", "account, app_name, action", "Total Access login requests."],
    ["gateway_dns_queries_total", "counter", "account, location, policy, category, action", "Total Gateway DNS queries."],
    ["gateway_network_sessions_total", "counter", "account, policy, action", "Total Gateway network sessions."],
    ["gateway_network_bytes_total", "counter", "account, policy, direction", "Total bytes through Gateway network sessions."],
    ["browser_isolation_sessions_total", "counter", "account", "Total Browser Isolation sessions."],
    ["tunnel_requests_total", "counter", "account, tunnel_name", "Total requests proxied through a Cloudflare Tunnel."],
    ["tunnel_info", "gauge", "account, tunnel_name, tunnel_id, status", "Tunnel metadata; value is always 1."],
  ];

  const dnsMetrics = [
    ["dns_queries_total", "counter", "zone, query_type, response_code", "Total authoritative DNS queries for a zone."],
    ["dns_query_duration_seconds", "histogram", "zone", "DNS query processing time distribution."],
    ["dns_firewall_queries_total", "counter", "zone, cluster, response_code", "Total queries handled by DNS Firewall."],
  ];

  const domainMetrics = [
    ["domain_expiration_timestamp_seconds", "gauge", "zone, domain", "Unix timestamp when the domain registration expires."],
    ["domain_auto_renew", "gauge", "zone, domain", "1 if auto-renew is enabled, 0 otherwise."],
    ["domain_locked", "gauge", "zone, domain", "1 if the domain is locked, 0 otherwise."],
    ["zone_status", "gauge", "zone, status", "1 for the current zone status (active, pending, etc.)."],
    ["certificate_expiration_timestamp_seconds", "gauge", "zone, hostname, issuer", "Unix timestamp when the SSL certificate expires."],
  ];

  const selfMetrics = [
    ["cloudflare_exporter_scrape_duration_seconds", "histogram", "Time spent collecting metrics from Cloudflare per cycle."],
    ["cloudflare_exporter_scrape_errors_total", "counter", "Total collection errors by collector name."],
    ["cloudflare_exporter_graphql_requests_total", "counter", "Total GraphQL API requests made."],
    ["cloudflare_exporter_rest_requests_total", "counter", "Total REST API requests made."],
    ["cloudflare_exporter_graphql_budget_remaining", "gauge", "Remaining GraphQL calls in the current budget window."],
    ["cloudflare_exporter_rest_budget_remaining", "gauge", "Remaining REST calls in the current budget window."],
    ["cloudflare_exporter_discovery_runs_total", "counter", "Total capability discovery runs."],
    ["cloudflare_exporter_active_collectors", "gauge", "Number of currently active collectors."],
    ["cloudflare_exporter_build_info", "gauge", "Build metadata (version, commit, Go version). Value is always 1."],
  ];

  const renderMetricTable = (metrics: string[][]): string => {
    if (metrics[0].length === 4) {
      const rows = metrics
        .map(([name, type, labels, desc]) => `<tr><td><code>${name}</code></td><td>${type}</td><td><code>${labels}</code></td><td>${desc}</td></tr>`)
        .join("");
      return `<table><thead><tr><th>Metric</th><th>Type</th><th>Labels</th><th>Description</th></tr></thead><tbody>${rows}</tbody></table>`;
    }
    const rows = metrics
      .map(([name, type, desc]) => `<tr><td><code>${name}</code></td><td>${type}</td><td>${desc}</td></tr>`)
      .join("");
    return `<table><thead><tr><th>Metric</th><th>Type</th><th>Description</th></tr></thead><tbody>${rows}</tbody></table>`;
  };

  return `
    <div class="section">
      <h2>Cloudflare Exporter</h2>
      <p>
        <code>cloudflare_exporter</code> collects metrics from the Cloudflare API
        (GraphQL Analytics and REST) and presents them on a standard <code>/metrics</code>
        endpoint. It is designed to <strong>complement, not replace</strong> the Cloudflare
        dashboard: it exposes the subset of Cloudflare telemetry that is useful for alerting
        and capacity planning inside a Prometheus/Grafana stack, while leaving full-fidelity
        log analysis to Cloudflare's own tools.
      </p>
      <p>
        The exporter runs as a single stateless container. It discovers the accounts, zones,
        and products available to the configured API token, then schedules collection cycles
        that stay within Cloudflare's API rate limits.
      </p>
    </div>

    <div class="section">
      <h2>Architecture</h2>
      <p>The exporter consists of four main subsystems:</p>
      <table>
        <thead><tr><th>Subsystem</th><th>Description</th></tr></thead>
        <tbody>
          <tr>
            <td><strong>Capability Discovery</strong></td>
            <td>Probes the API token's permissions and account entitlements on startup and periodically thereafter. Only collectors whose prerequisites are satisfied are activated.</td>
          </tr>
          <tr>
            <td><strong>Scheduler</strong></td>
            <td>Triggers each enabled collector at the configured refresh interval. Collectors are staggered to spread API calls over time rather than bursting.</td>
          </tr>
          <tr>
            <td><strong>Quota Governor</strong></td>
            <td>Two token-bucket governors independently track GraphQL and REST call budgets. When a budget is exhausted, the scheduler delays collection rather than returning errors.</td>
          </tr>
          <tr>
            <td><strong>Aggregation Store</strong></td>
            <td>In-memory store holding one generation of data per metric family. Prometheus scrapes serialize the current generation. No persistent state; a restart rebuilds from the next collection cycle.</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="section">
      <h2>Installation</h2>
      <p>Container images are published to GitHub Container Registry:</p>
      <pre><code>ghcr.io/phaseshiftdata/cloudflare_exporter</code></pre>
      <pre><code># Run with an API token
docker run -d \\
  --name cloudflare_exporter \\
  -p 9199:9199 \\
  -e CF_API_TOKEN="your-cloudflare-api-token" \\
  ghcr.io/phaseshiftdata/cloudflare_exporter:main</code></pre>
    </div>

    <div class="section">
      <h2>Configuration</h2>
      <p>All configuration is via environment variables. No configuration file is required.</p>
      <table>
        <thead><tr><th>Variable</th><th>Default</th><th>Description</th></tr></thead>
        <tbody>${configTable}</tbody>
      </table>
    </div>

    <div class="section">
      <h2>Secret Configuration</h2>
      <p>
        Credentials such as the Cloudflare API token can be provided in three
        mutually exclusive ways. Setting more than one source for the same
        secret is a configuration error.
      </p>

      <h3>Flag / Environment Variable</h3>
      <p>
        Pass the secret directly via <code>--cf.api-token</code> or
        <code>CF_API_TOKEN</code>. This is the simplest approach but the value
        is visible in process listings and the environment.
      </p>

      <h3>File-Based Secrets</h3>
      <p>
        Each credential flag has a <code>-file</code> variant that reads the
        value from a file at startup. Trailing whitespace and newlines are
        trimmed. The exporter exits with an error if the file is missing,
        unreadable, or empty after trimming.
      </p>
      <table>
        <thead><tr><th>Flag</th><th>File Variant</th><th>Description</th></tr></thead>
        <tbody>
          <tr><td><code>--cf.api-token</code></td><td><code>--cf.api-token-file</code></td><td>Cloudflare API token</td></tr>
          <tr><td><code>--web.basic-auth-password</code></td><td><code>--web.basic-auth-password-file</code></td><td>Basic auth password for the metrics endpoint</td></tr>
        </tbody>
      </table>

      <h3>OpenBao-Backed Secrets</h3>
      <p>
        For deployments using <a href="https://openbao.org/">OpenBao</a> (or
        HashiCorp Vault) as a central secret store, the exporter can read
        credentials directly from a KV v2 secret engine. This avoids placing
        secrets on disk entirely.
      </p>
      <table>
        <thead><tr><th>Flag</th><th>Description</th></tr></thead>
        <tbody>
          <tr><td><code>--openbao-address</code></td><td>OpenBao/Vault server address (env: <code>OPENBAO_ADDR</code>).</td></tr>
          <tr><td><code>--openbao-approle-role-id-file</code></td><td>Path to a file containing the AppRole <code>role_id</code>.</td></tr>
          <tr><td><code>--openbao-approle-secret-id-file</code></td><td>Path to a file containing the AppRole <code>secret_id</code>.</td></tr>
          <tr><td><code>--cf.api-token-openbao=&lt;path&gt;:&lt;field&gt;</code></td><td>Read the API token from the given KV v2 path and field.</td></tr>
        </tbody>
      </table>
      <p>
        The reference format is <code>&lt;kv-path&gt;:&lt;field&gt;</code>, for
        example <code>secret/cloudflare/exporter:api_token</code>. If the vault
        is sealed or unreachable at startup, the exporter retries with
        exponential backoff in the background. A background goroutine
        automatically renews the AppRole token before it expires.
      </p>

      <h3>Summary</h3>
      <table>
        <thead><tr><th>Credential</th><th>Flag / Env</th><th>File</th><th>OpenBao</th></tr></thead>
        <tbody>
          <tr>
            <td>Cloudflare API token</td>
            <td><code>--cf.api-token</code> / <code>CF_API_TOKEN</code></td>
            <td><code>--cf.api-token-file</code></td>
            <td><code>--cf.api-token-openbao</code></td>
          </tr>
          <tr>
            <td>Basic-auth password</td>
            <td><code>--web.basic-auth-password</code></td>
            <td><code>--web.basic-auth-password-file</code></td>
            <td><em>not yet supported</em></td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="section">
      <h2>API Token Setup</h2>
      <p>The exporter requires a Cloudflare API <strong>token</strong> (not a Global API Key). Create one under <strong>My Profile &gt; API Tokens</strong> in the Cloudflare dashboard.</p>
      <table>
        <thead><tr><th>Permission</th><th>Access</th><th>Needed For</th></tr></thead>
        <tbody>
          <tr><td>Account &rarr; Access: Apps and Policies</td><td>Read</td><td>Zero Trust Access login metrics</td></tr>
          <tr><td>Account &rarr; Zero Trust</td><td>Read</td><td>Gateway DNS, network session, and browser isolation metrics</td></tr>
          <tr><td>Account &rarr; Cloudflare Tunnel</td><td>Read</td><td>Tunnel status and request metrics</td></tr>
          <tr><td>Zone &rarr; Analytics</td><td>Read</td><td>DNS analytics (GraphQL)</td></tr>
          <tr><td>Zone &rarr; DNS</td><td>Read</td><td>DNS Firewall metrics</td></tr>
          <tr><td>Zone &rarr; Zone</td><td>Read</td><td>Zone status, domain metadata</td></tr>
          <tr><td>Zone &rarr; SSL and Certificates</td><td>Read</td><td>Certificate expiration monitoring</td></tr>
        </tbody>
      </table>
    </div>

    <div class="section">
      <h2>Metrics: Zero Trust</h2>
      ${renderMetricTable(ztMetrics)}
    </div>

    <div class="section">
      <h2>Metrics: DNS</h2>
      ${renderMetricTable(dnsMetrics)}
    </div>

    <div class="section">
      <h2>Metrics: Domain &amp; Certificate Lifecycle</h2>
      ${renderMetricTable(domainMetrics)}
    </div>

    <div class="section">
      <h2>Metrics: Self-Instrumentation</h2>
      ${renderMetricTable(selfMetrics)}
    </div>

    <div class="section">
      <h2>Capability Discovery</h2>
      <p>
        On startup (and every <code>CF_DISCOVERY_INTERVAL_SECONDS</code> thereafter) the
        exporter inspects the API token's permissions and the account's entitled products.
        Only collectors whose prerequisites are satisfied are activated. This means a single
        binary works for Free plans (REST-only floor) through Enterprise (full GraphQL
        Analytics access) without manual feature flags.
      </p>
      <h3>CLI Flag</h3>
      <pre><code>docker run --rm \\
  -e CF_API_TOKEN="your-token" \\
  ghcr.io/phaseshiftdata/cloudflare_exporter:main \\
  --capabilities</code></pre>
      <p>This prints a JSON summary of discovered capabilities and exits.</p>
      <h3>HTTP Endpoint</h3>
      <pre><code>curl http://localhost:9199/capabilities</code></pre>
      <p>Returns the same JSON summary while the exporter is running.</p>
    </div>

    <div class="section">
      <h2>Collection Model</h2>
      <p>
        Each collection cycle queries a fixed-width time window (<code>CF_TIME_WINDOW_SECONDS</code>)
        that ends at <code>now - CF_SCRAPE_DELAY_SECONDS</code>. The five-minute default delay
        accounts for Cloudflare's analytics pipeline propagation latency; queries against more
        recent data risk returning incomplete results.
      </p>
      <p>
        Counters (such as <code>dns_queries_total</code>) are accumulated from the per-window deltas
        returned by the API. On restart the counters reset to zero and rebuild from the first
        successful collection. Prometheus <code>rate()</code> and <code>increase()</code> handle
        this naturally.
      </p>
      <p>
        The aggregation store keys each sample by its full label set. If a collection cycle returns
        a sample with the same labels as an existing entry, the new value replaces the old one,
        preventing double-counting when windows overlap.
      </p>
    </div>

    <div class="section">
      <h2>Cardinality and Privacy</h2>
      <p>The exporter deliberately omits high-cardinality and privacy-sensitive dimensions:</p>
      <ul>
        <li><strong>No per-IP breakdowns.</strong> Source and destination IPs are never used as label values.</li>
        <li><strong>No query-name labels.</strong> DNS query names (QNAMEs) are not exposed.</li>
        <li><strong>No per-URL path labels.</strong> HTTP request paths are not included.</li>
      </ul>
      <p>
        The <code>CF_GATEWAY_CATEGORY_TOP_N</code> setting caps the number of distinct category label
        values per cycle, providing a hard cardinality ceiling on the highest-cardinality metric
        family (<code>gateway_dns_queries_total</code>).
      </p>
    </div>

    <div class="section">
      <h2>Failure Modes</h2>
      <table>
        <thead><tr><th>Scenario</th><th>Behavior</th></tr></thead>
        <tbody>
          <tr>
            <td><strong>Single collector failure</strong></td>
            <td>Error is logged, <code>cloudflare_exporter_scrape_errors_total</code> incremented. Other collectors continue. The failed collector retries on the next cycle.</td>
          </tr>
          <tr>
            <td><strong>Stale-but-served</strong></td>
            <td>When a collector fails, the aggregation store continues to serve the previous generation of that collector's metrics. Prometheus sees stale but valid data rather than a scrape failure.</td>
          </tr>
          <tr>
            <td><strong>Budget exhaustion</strong></td>
            <td>Collectors are delayed rather than dropped. Metrics remain at their last-known values until the budget refills. Monitor <code>cloudflare_exporter_*_budget_remaining</code> gauges.</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="section">
      <h2>Endpoints</h2>
      <table>
        <thead><tr><th>Path</th><th>Method</th><th>Description</th></tr></thead>
        <tbody>
          <tr><td><code>/metrics</code></td><td>GET</td><td>Prometheus metrics endpoint.</td></tr>
          <tr><td><code>/health</code></td><td>GET</td><td>Health check. Returns 200 OK when the exporter is running.</td></tr>
          <tr><td><code>/capabilities</code></td><td>GET</td><td>JSON summary of discovered capabilities and active collectors.</td></tr>
        </tbody>
      </table>
    </div>

    <div class="section">
      <h2>Deployment</h2>
      <h3>Kubernetes</h3>
      <pre><code>apiVersion: apps/v1
kind: Deployment
metadata:
  name: cloudflare-exporter
spec:
  replicas: 1
  selector:
    matchLabels:
      app: cloudflare-exporter
  template:
    metadata:
      labels:
        app: cloudflare-exporter
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9199"
    spec:
      containers:
        - name: cloudflare-exporter
          image: ghcr.io/phaseshiftdata/cloudflare_exporter:main
          ports:
            - containerPort: 9199
          env:
            - name: CF_API_TOKEN
              valueFrom:
                secretKeyRef:
                  name: cloudflare-exporter
                  key: api-token
          livenessProbe:
            httpGet:
              path: /health
              port: 9199
          readinessProbe:
            httpGet:
              path: /health
              port: 9199</code></pre>

      <h3>Prometheus Scrape Config</h3>
      <pre><code>scrape_configs:
  - job_name: cloudflare
    scrape_interval: 60s
    static_configs:
      - targets:
          - cloudflare-exporter:9199</code></pre>
      <p>Align <code>scrape_interval</code> with <code>CF_REFRESH_INTERVAL_SECONDS</code> to avoid scraping between collection cycles.</p>

      <h3>Single-Replica Topology</h3>
      <p>
        The exporter is designed to run as a <strong>single replica</strong>. Running multiple
        replicas against the same API token multiplies API call volume and may exhaust rate limits.
        If high availability is required, run one active replica with a standby that only starts
        if the active fails.
      </p>
    </div>

    <div class="section">
      <h2>Free Plan Considerations</h2>
      <ul>
        <li><strong>GraphQL Analytics may be unavailable.</strong> The exporter falls back to REST-only collectors automatically via capability discovery.</li>
        <li><strong>Zero Trust metrics require a Zero Trust subscription.</strong> Access, Gateway, Browser Isolation, and Tunnel collectors are disabled when the account lacks a Zero Trust plan.</li>
        <li><strong>Domain registration metrics</strong> require domains registered through Cloudflare Registrar.</li>
        <li><strong>DNS Firewall metrics</strong> require a DNS Firewall subscription.</li>
      </ul>
      <p>The REST-only floor provides zone status, certificate expiration, and domain metadata metrics on all plan levels, including Free.</p>
    </div>`;
}
