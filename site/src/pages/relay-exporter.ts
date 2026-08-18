export function RelayExporterPage(): string {
  const configRows = [
    ["--listen-address", "127.0.0.1:9100", "Address and port the HTTP server listens on."],
    ["--allowed-source", "<em>(required)</em>", "Single IP address allowed to send scrape requests. The relay refuses to start if omitted."],
    ["--tls-cert-file", "<em>(optional)</em>", "TLS certificate for the relay listener. When provided with <code>--tls-key-file</code>, the relay serves HTTPS."],
    ["--tls-key-file", "<em>(optional)</em>", "TLS private key for the relay listener. When provided with <code>--tls-cert-file</code>, the relay serves HTTPS."],
    ["--ca-cert", "<em>(optional)</em>", "CA certificate bundle for verifying target TLS certificates."],
    ["--tls-skip-verify", "false", "Skip TLS certificate verification when connecting to targets."],
    ["--proxy-timeout", "10s", "Timeout for proxy requests to targets."],
    ["--concurrent-requests", "100", "Maximum number of concurrent proxy requests."],
    ["--log-level", "info", "Log verbosity. One of <code>debug</code>, <code>info</code>, <code>warn</code>, <code>error</code>."],
  ];

  const configTable = configRows
    .map(([flag, def, desc]) => `<tr><td><code>${flag}</code></td><td><code>${def}</code></td><td>${desc}</td></tr>`)
    .join("");

  const statusCodes = [
    ["Relay functioning, target responded (any status)", "<strong>200</strong> (target status reported in <code>relay_target_http_status</code>)"],
    ["Relay functioning, target timed out or unreachable", "<strong>200</strong> (with <code>relay_target_response 0</code>, <code>relay_target_http_status 0</code>)"],
    ["Source IP not allowed", "<strong>403</strong> Forbidden"],
    ["Missing or invalid query parameters", "<strong>400</strong> Bad Request"],
    ["Concurrent request limit exceeded", "<strong>429</strong> Too Many Requests"],
  ];

  const statusTable = statusCodes
    .map(([scenario, status]) => `<tr><td>${scenario}</td><td>${status}</td></tr>`)
    .join("");

  const relayMetrics = [
    ["relay_response", "gauge", "<em>(none)</em>", "Whether the relay itself is functioning (always 1 when the relay returns HTTP 200)."],
    ["relay_target_response", "gauge", "<em>(none)</em>", "Whether the target responded successfully (1 = success, 0 = timeout or unreachable)."],
    ["relay_target_http_status", "gauge", "<em>(none)</em>", "HTTP status code returned by the target (0 when the target is unreachable)."],
  ];

  const renderMetricTable = (metrics: string[][]): string => {
    const rows = metrics
      .map(([name, type, labels, desc]) => `<tr><td><code>${name}</code></td><td>${type}</td><td><code>${labels}</code></td><td>${desc}</td></tr>`)
      .join("");
    return `<table><thead><tr><th>Metric</th><th>Type</th><th>Labels</th><th>Description</th></tr></thead><tbody>${rows}</tbody></table>`;
  };

  return `
    <div class="section">
      <h2>Relay Exporter</h2>
      <p>
        <code>relay_exporter</code> is a Prometheus metrics relay proxy for
        RFC 1918 targets behind VPN tunnels or private networks. Prometheus
        scrapes the relay, which fetches <code>/metrics</code> from targets
        that Prometheus cannot directly reach. The relay validates every
        request, enforces source IP filtering, and restricts targets to
        private address ranges so it cannot be used as an open proxy.
      </p>
      <p>
        The relay always returns HTTP 200 to Prometheus when it is functioning
        correctly. The target's actual HTTP status and reachability are
        reported via gauge metrics appended to the response body.
      </p>
    </div>

    <div class="section">
      <h2>Architecture</h2>
      <p>
        The relay acts as a proxy between Prometheus and targets on private
        networks. Prometheus sends scrape requests to the relay with query
        parameters specifying the target IP, port, and TLS preference. The
        relay fetches <code>/metrics</code> from the target and returns
        the response with relay status metrics appended.
      </p>
      <pre><code>Prometheus --&gt; relay_exporter --&gt; target (RFC 1918 host)
               (network A)         (network B)</code></pre>
      <h3>Request Flow</h3>
      <ol>
        <li>Prometheus sends an HTTP(S) GET to the relay at <code>/metrics?ip=&lt;target_ip&gt;&amp;port=&lt;number&gt;&amp;tls=&lt;true|false&gt;</code></li>
        <li>The relay validates the request (source IP, target IP, port)</li>
        <li>The relay proxies the request to <code>http(s)://&lt;ip&gt;:&lt;port&gt;/metrics</code></li>
        <li>The relay returns HTTP 200 with the target's response body and relay status metrics appended</li>
      </ol>
    </div>

    <div class="section">
      <h2>Installation</h2>
      <p>Container images are published to GitHub Container Registry:</p>
      <pre><code>ghcr.io/phaseshiftdata/relay_exporter</code></pre>
      <pre><code># Run the relay exporter
docker run -d --rm \\
  --name relay_exporter \\
  -p 9100:9100 \\
  ghcr.io/phaseshiftdata/relay_exporter:main \\
  --listen-address=0.0.0.0:9100 \\
  --allowed-source=203.0.113.10</code></pre>
    </div>

    <div class="section">
      <h2>Configuration</h2>
      <p>All configuration is via CLI flags. No configuration file or environment variables are required.</p>
      <table>
        <thead><tr><th>Flag</th><th>Default</th><th>Description</th></tr></thead>
        <tbody>${configTable}</tbody>
      </table>
    </div>

    <div class="section">
      <h2>Request Validation</h2>
      <h3>Source IP Filtering (HTTP 403)</h3>
      <p>
        Only the IP address specified by <code>--allowed-source</code> may send
        requests. All other source IPs receive HTTP 403 Forbidden. The
        <code>--allowed-source</code> flag is <strong>required</strong> &mdash;
        <code>relay_exporter</code> refuses to start with a clear error message
        if it is omitted.
      </p>
      <h3>RFC 1918 Target Validation (HTTP 400)</h3>
      <p>The target <code>ip</code> must be a valid RFC 1918 private address:</p>
      <ul>
        <li><code>10.0.0.0/8</code></li>
        <li><code>172.16.0.0/12</code></li>
        <li><code>192.168.0.0/16</code></li>
      </ul>
      <p>
        The <code>port</code> parameter is required and must be 1&ndash;65535
        (port 0 is explicitly disallowed). The <code>tls</code> parameter is
        optional, defaults to <code>false</code>, and must be exactly
        <code>true</code> or <code>false</code>. Missing or invalid parameters
        return HTTP 400 Bad Request.
      </p>
    </div>

    <div class="section">
      <h2>Proxy Behavior</h2>
      <p>
        When validation passes, <code>relay_exporter</code> sends an HTTP GET to
        <code>http(s)://&lt;ip&gt;:&lt;port&gt;/metrics</code> on behalf of
        Prometheus. The <code>Authorization</code> header from the Prometheus
        request is forwarded to the target if present. HTTPS is used for the
        target connection when <code>tls=true</code>.
      </p>
    </div>

    <div class="section">
      <h2>Response Format</h2>
      <p>
        Responses use standard Prometheus text format with <code># HELP</code>
        and <code># TYPE</code> headers. The relay appends three gauge metrics
        after the target's response body.
      </p>
      ${renderMetricTable(relayMetrics)}
      <h3>Successful Target Response</h3>
      <pre><code># (target's /metrics body verbatim)

# HELP relay_response Whether the relay itself is functioning
# TYPE relay_response gauge
relay_response 1
# HELP relay_target_response Whether the target responded successfully
# TYPE relay_target_response gauge
relay_target_response 1
# HELP relay_target_http_status HTTP status code returned by the target
# TYPE relay_target_http_status gauge
relay_target_http_status 200</code></pre>
      <h3>Target Timeout or Connection Failure</h3>
      <pre><code># HELP relay_response Whether the relay itself is functioning
# TYPE relay_response gauge
relay_response 1
# HELP relay_target_response Whether the target responded successfully
# TYPE relay_target_response gauge
relay_target_response 0
# HELP relay_target_http_status HTTP status code returned by the target
# TYPE relay_target_http_status gauge
relay_target_http_status 0</code></pre>
    </div>

    <div class="section">
      <h2>HTTP Status Codes</h2>
      <p>
        The relay always returns HTTP 200 to Prometheus when it is functioning
        correctly. The target's actual HTTP status is reported via
        <code>relay_target_http_status</code> in the response body.
      </p>
      <table>
        <thead><tr><th>Scenario</th><th>Status Returned to Prometheus</th></tr></thead>
        <tbody>${statusTable}</tbody>
      </table>
    </div>

    <div class="section">
      <h2>TLS</h2>
      <h3>Relay Listener TLS (Optional)</h3>
      <p>
        If <code>--tls-cert-file</code> and <code>--tls-key-file</code> are both
        provided, the relay serves HTTPS. Otherwise it serves HTTP.
      </p>
      <h3>Target Connection TLS</h3>
      <p>
        When <code>tls=true</code> is passed as a query parameter, the relay
        connects to the target over HTTPS. Target certificate verification
        uses the system CA bundle by default. Use <code>--ca-cert</code> to
        provide a custom CA certificate bundle, or <code>--tls-skip-verify</code>
        to skip verification entirely.
      </p>
    </div>

    <div class="section">
      <h2>Prometheus Configuration</h2>
      <p>
        The relay uses the multi-target exporter pattern. Prometheus discovers
        targets and passes their address to the relay via relabel configuration.
      </p>
      <pre><code>scrape_configs:
  - job_name: relay
    scrape_interval: 30s
    static_configs:
      - targets:
          - 10.0.0.1:9100
          - 10.0.0.2:9100
          - 192.168.1.10:9100
    relabel_configs:
      - source_labels: [__address__]
        regex: "([^:]+):(\\\\d+)"
        target_label: __param_ip
        replacement: "\${1}"
      - source_labels: [__address__]
        regex: "([^:]+):(\\\\d+)"
        target_label: __param_port
        replacement: "\${2}"
      - source_labels: [__address__]
        target_label: instance
      - target_label: __address__
        replacement: "203.0.113.5:9100"</code></pre>
      <p>
        In this configuration, Prometheus sends all scrape requests to the
        relay at <code>203.0.113.5:9100</code>, which proxies them to the
        private targets listed in <code>static_configs</code>.
      </p>
    </div>

    <div class="section">
      <h2>Endpoints</h2>
      <table>
        <thead><tr><th>Path</th><th>Method</th><th>Description</th></tr></thead>
        <tbody>
          <tr><td><code>/metrics?ip=...&amp;port=...&amp;tls=...</code></td><td>GET</td><td>Proxy endpoint. Fetches metrics from the specified target and returns the response with relay status metrics appended.</td></tr>
          <tr><td><code>/health</code></td><td>GET</td><td>Health and readiness check for liveness probes.</td></tr>
          <tr><td><code>/</code></td><td>GET</td><td>Landing page with links to other endpoints.</td></tr>
        </tbody>
      </table>
    </div>

    <div class="section">
      <h2>Deployment</h2>
      <h3>Kubernetes</h3>
      <pre><code>apiVersion: apps/v1
kind: Deployment
metadata:
  name: relay-exporter
spec:
  replicas: 1
  selector:
    matchLabels:
      app: relay-exporter
  template:
    metadata:
      labels:
        app: relay-exporter
    spec:
      containers:
        - name: relay-exporter
          image: ghcr.io/phaseshiftdata/relay_exporter:main
          args:
            - --listen-address=0.0.0.0:9100
            - --allowed-source=203.0.113.10
          ports:
            - containerPort: 9100
---
apiVersion: v1
kind: Service
metadata:
  name: relay-exporter
spec:
  selector:
    app: relay-exporter
  ports:
    - port: 9100
      targetPort: 9100</code></pre>

      <h3>Docker Compose</h3>
      <pre><code>services:
  relay_exporter:
    image: ghcr.io/phaseshiftdata/relay_exporter:main
    ports:
      - "9100:9100"
    command:
      - --listen-address=0.0.0.0:9100
      - --allowed-source=203.0.113.10
    restart: unless-stopped</code></pre>
    </div>

    <div class="section">
      <h2>Failure Modes</h2>
      <table>
        <thead><tr><th>Scenario</th><th>Behavior</th></tr></thead>
        <tbody>
          <tr>
            <td><strong>Target unreachable</strong></td>
            <td>The relay returns HTTP 200 with <code>relay_target_response 0</code> and <code>relay_target_http_status 0</code>. The <code>relay_response 1</code> metric confirms the relay itself is functioning.</td>
          </tr>
          <tr>
            <td><strong>Proxy timeout</strong></td>
            <td>When the target does not respond within the <code>--proxy-timeout</code> window (default 10s), the relay treats it the same as an unreachable target: HTTP 200 with <code>relay_target_response 0</code> and <code>relay_target_http_status 0</code>.</td>
          </tr>
          <tr>
            <td><strong>Concurrent request limit exceeded</strong></td>
            <td>When the number of in-flight proxy requests exceeds <code>--concurrent-requests</code> (default 100), the relay returns HTTP 429 Too Many Requests.</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="section">
      <h2>Security</h2>
      <table>
        <thead><tr><th>Control</th><th>Description</th></tr></thead>
        <tbody>
          <tr>
            <td><strong>Source IP filtering</strong></td>
            <td>Only the IP specified by <code>--allowed-source</code> may send requests. All other sources receive HTTP 403.</td>
          </tr>
          <tr>
            <td><strong>RFC 1918 restriction</strong></td>
            <td>The relay only proxies requests to private IP addresses (<code>10.0.0.0/8</code>, <code>172.16.0.0/12</code>, <code>192.168.0.0/16</code>). Public IP addresses are rejected with HTTP 400.</td>
          </tr>
          <tr>
            <td><strong>No open proxy</strong></td>
            <td>The combination of source IP filtering, RFC 1918 target restriction, and fixed <code>/metrics</code> path prevents the relay from being used as an open proxy.</td>
          </tr>
          <tr>
            <td><strong>Authorization forwarding</strong></td>
            <td>The <code>Authorization</code> header is forwarded from Prometheus to the target, supporting bearer token authentication on targets.</td>
          </tr>
        </tbody>
      </table>
    </div>`;
}
