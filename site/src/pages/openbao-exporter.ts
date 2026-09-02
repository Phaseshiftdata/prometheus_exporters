export function OpenBaoExporterPage(): string {
  const configRows = [
    ["--listen-address", "127.0.0.1:9100", "Address and port the HTTP server listens on."],
    ["--openbao-addr", "<em>(required)</em>", "OpenBao API address (e.g., <code>https://openbao:8200</code>)."],
    ["--openbao-token", "<em>(optional)</em>", "Authentication token for OpenBao API."],
    ["--openbao-token-file", "<em>(optional)</em>", "Path to file containing authentication token."],
    ["--log-level", "info", "Log verbosity. One of <code>debug</code>, <code>info</code>, <code>warn</code>, <code>error</code>."],
    ["--poll-interval", "30s", "How often to re-discover cluster members."],
  ];

  const configTable = configRows
    .map(([flag, def, desc]) => `<tr><td><code>${flag}</code></td><td><code>${def}</code></td><td>${desc}</td></tr>`)
    .join("");

  const healthMetrics = [
    ["openbao_up", "gauge", "<em>(none)</em>", "1 if the seed node is reachable."],
    ["openbao_initialized", "gauge", "<em>(none)</em>", "1 if the cluster is initialized."],
    ["openbao_sealed", "gauge", "node", "1 if the node is sealed."],
    ["openbao_standby", "gauge", "node", "1 if the node is in standby mode."],
    ["openbao_leader", "gauge", "node", "1 on the leader node."],
    ["openbao_node_info", "gauge", "node, version", "Information about an OpenBao node."],
  ];

  const clusterMetrics = [
    ["openbao_raft_committed_index", "gauge", "<em>(none)</em>", "Raft committed index."],
    ["openbao_raft_applied_index", "gauge", "<em>(none)</em>", "Raft applied index."],
    ["openbao_peers", "gauge", "<em>(none)</em>", "Number of raft peers."],
  ];

  const renderMetricTable = (metrics: string[][]): string => {
    const rows = metrics
      .map(([name, type, labels, desc]) => `<tr><td><code>${name}</code></td><td>${type}</td><td><code>${labels}</code></td><td>${desc}</td></tr>`)
      .join("");
    return `<table><thead><tr><th>Metric</th><th>Type</th><th>Labels</th><th>Description</th></tr></thead><tbody>${rows}</tbody></table>`;
  };

  const healthStatusCodes = [
    ["200", "Active (initialized, unsealed, leader)"],
    ["429", "Standby"],
    ["472", "DR secondary standby"],
    ["501", "Uninitialized"],
    ["503", "Sealed"],
  ];

  const statusTable = healthStatusCodes
    .map(([code, desc]) => `<tr><td><code>${code}</code></td><td>${desc}</td></tr>`)
    .join("");

  return `
    <div class="section">
      <h2>OpenBao Exporter</h2>
      <p>
        <code>openbao_exporter</code> is a Prometheus exporter for OpenBao
        cluster metrics. It connects to a single OpenBao seed node,
        collects health status and native metrics, discovers cluster
        members via raft configuration, and re-exposes everything at
        <code>/metrics</code> in standard Prometheus text format.
      </p>
      <p>
        OpenBao exposes metrics at
        <code>/v1/sys/metrics?format=prometheus</code> and health at
        <code>/v1/sys/health</code>, but neither is at the standard
        <code>/metrics</code> path. This exporter bridges that gap.
      </p>
    </div>

    <div class="section">
      <h2>Architecture</h2>
      <p>
        The exporter connects to a configured seed node and periodically
        discovers cluster members via the raft configuration API.
      </p>
      <pre><code>Prometheus --&gt; openbao_exporter --&gt; OpenBao seed node
                                 --&gt; OpenBao node 2 (discovered)
                                 --&gt; OpenBao node 3 (discovered)</code></pre>
      <h3>Collection Flow</h3>
      <ol>
        <li>On startup, the exporter connects to the configured seed node.</li>
        <li>It periodically discovers cluster members via <code>/v1/sys/storage/raft/configuration</code>.</li>
        <li>On each scrape, it fetches <code>/v1/sys/health</code> for node status.</li>
        <li>It fetches <code>/v1/sys/metrics?format=prometheus</code> for native metrics.</li>
        <li>All metrics are combined and served at <code>/metrics</code>.</li>
      </ol>
    </div>

    <div class="section">
      <h2>Installation</h2>
      <p>Container images are published to GitHub Container Registry:</p>
      <pre><code>ghcr.io/phaseshiftdata/openbao_exporter</code></pre>
      <pre><code># Run the OpenBao exporter
docker run -d --rm \\
  --name openbao_exporter \\
  -p 9100:9100 \\
  ghcr.io/phaseshiftdata/openbao_exporter:main \\
  --listen-address=0.0.0.0:9100 \\
  --openbao-addr=https://openbao:8200</code></pre>
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
      <h2>Metrics: Health</h2>
      ${renderMetricTable(healthMetrics)}
    </div>

    <div class="section">
      <h2>Metrics: Cluster</h2>
      ${renderMetricTable(clusterMetrics)}
    </div>

    <div class="section">
      <h2>Native Metrics</h2>
      <p>
        All metrics from <code>/v1/sys/metrics?format=prometheus</code>
        are passed through. These include runtime statistics, storage
        backend metrics, and other internal OpenBao telemetry.
      </p>
    </div>

    <div class="section">
      <h2>Health Endpoint Status Codes</h2>
      <p>
        The <code>/v1/sys/health</code> endpoint returns different HTTP
        status codes for different node states, but always returns a JSON
        body. The exporter treats all of these as valid responses.
      </p>
      <table>
        <thead><tr><th>Status Code</th><th>Node State</th></tr></thead>
        <tbody>${statusTable}</tbody>
      </table>
    </div>

    <div class="section">
      <h2>Cluster Discovery</h2>
      <p>
        The exporter discovers cluster members via
        <code>/v1/sys/storage/raft/configuration</code>. This endpoint
        requires authentication and may not be available on all
        deployments. When unavailable, the exporter reports only the seed
        node with <code>openbao_peers 1</code>.
      </p>
    </div>

    <div class="section">
      <h2>Endpoints</h2>
      <table>
        <thead><tr><th>Path</th><th>Method</th><th>Description</th></tr></thead>
        <tbody>
          <tr><td><code>/metrics</code></td><td>GET</td><td>Prometheus metrics endpoint.</td></tr>
          <tr><td><code>/</code></td><td>GET</td><td>Landing page with link to metrics.</td></tr>
        </tbody>
      </table>
    </div>

    <div class="section">
      <h2>Deployment</h2>
      <h3>Docker Compose</h3>
      <pre><code>services:
  openbao_exporter:
    image: ghcr.io/phaseshiftdata/openbao_exporter:main
    ports:
      - "9100:9100"
    command:
      - --listen-address=0.0.0.0:9100
      - --openbao-addr=https://openbao:8200
      - --openbao-token-file=/run/secrets/openbao_token
    restart: unless-stopped</code></pre>

      <h3>Kubernetes</h3>
      <pre><code>apiVersion: apps/v1
kind: Deployment
metadata:
  name: openbao-exporter
spec:
  replicas: 1
  selector:
    matchLabels:
      app: openbao-exporter
  template:
    metadata:
      labels:
        app: openbao-exporter
    spec:
      containers:
        - name: openbao-exporter
          image: ghcr.io/phaseshiftdata/openbao_exporter:main
          args:
            - --listen-address=0.0.0.0:9100
            - --openbao-addr=https://openbao:8200
          ports:
            - containerPort: 9100
---
apiVersion: v1
kind: Service
metadata:
  name: openbao-exporter
spec:
  selector:
    app: openbao-exporter
  ports:
    - port: 9100
      targetPort: 9100</code></pre>
    </div>

    <div class="section">
      <h2>Failure Modes</h2>
      <table>
        <thead><tr><th>Scenario</th><th>Behavior</th></tr></thead>
        <tbody>
          <tr>
            <td><strong>Seed node unreachable</strong></td>
            <td>The exporter reports <code>openbao_up 0</code> and no other metrics. The <code>/metrics</code> endpoint still returns HTTP 200.</td>
          </tr>
          <tr>
            <td><strong>Authentication failure</strong></td>
            <td>Health metrics are still collected (the health endpoint does not require authentication), but native metrics and cluster discovery may fail.</td>
          </tr>
          <tr>
            <td><strong>Raft not available</strong></td>
            <td>Cluster discovery returns 404 and the exporter reports only the seed node with <code>openbao_peers 1</code>.</td>
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
            <td><strong>Token handling</strong></td>
            <td>The authentication token is stored as <code>[]byte</code> and zeroed on shutdown.</td>
          </tr>
          <tr>
            <td><strong>Token file</strong></td>
            <td>Supports file-based token injection (Kubernetes secrets, Docker secrets) to avoid command-line exposure.</td>
          </tr>
          <tr>
            <td><strong>Distroless container</strong></td>
            <td>Runtime image is <code>gcr.io/distroless/static-debian12:nonroot</code> with no shell. Runs as UID 65532.</td>
          </tr>
          <tr>
            <td><strong>Response body limit</strong></td>
            <td>All API responses are capped at 100 MiB.</td>
          </tr>
        </tbody>
      </table>
    </div>`;
}
