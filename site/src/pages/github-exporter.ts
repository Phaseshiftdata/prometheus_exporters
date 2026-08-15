export function GitHubExporterPage(): string {
  const configRows = [
    ["--listen-address", "127.0.0.1:9102", "Address to listen on for metrics."],
    ["--database-url", "<em>(empty)</em>", "PostgreSQL connection string (or <code>DATABASE_URL</code> env)."],
    ["--database-password-file", "<em>(empty)</em>", "Path to file containing the database password (substituted into <code>--database-url</code>)."],
    ["--database-password-openbao", "<em>(empty)</em>", "OpenBao KV path:field for the database password."],
    ["--github-app-id", "0", "GitHub App ID."],
    ["--github-install-id", "0", "GitHub App Installation ID."],
    ["--github-key-file", "<em>(empty)</em>", "Path to GitHub App private key PEM file."],
    ["--poll-interval", "5m", "Polling interval for fresh data."],
    ["--org", "<em>(empty)</em>", "GitHub organization to monitor."],
    ["--log-level", "info", "Log level (debug, info, warn, error)."],
    ["--job-budget-per-poll", "50", "Maximum workflow-job requests one poll cycle may issue across all repos."],
    ["--backfill-interval", "2s", "Minimum spacing between historical backfill requests."],
    ["--backfill-min-rate-limit", "500", "Pause backfill when remaining GitHub rate limit is below this."],
    ["--openbao-address", "<em>(empty)</em>", "OpenBao/Vault server address (env: <code>OPENBAO_ADDR</code>)."],
    ["--openbao-approle-role-id-file", "<em>(empty)</em>", "Path to file containing the AppRole role_id."],
    ["--openbao-approle-secret-id-file", "<em>(empty)</em>", "Path to file containing the AppRole secret_id."],
  ];

  const configTable = configRows
    .map(([flag, def, desc]) => `<tr><td><code>${flag}</code></td><td><code>${def}</code></td><td>${desc}</td></tr>`)
    .join("");

  const cicdMetrics = [
    ["github_exporter_workflow_runs_total", "counter", "repo, workflow, conclusion", "Total workflow runs observed, by repo, workflow, and conclusion."],
    ["github_exporter_open_pull_requests", "gauge", "repo", "Current number of open pull requests per repo."],
    ["github_exporter_commits_total", "counter", "repo", "Total commits observed per repo."],
  ];

  const rateLimitMetrics = [
    ["github_exporter_rate_limit_remaining", "gauge", "", "GitHub API primary rate limit remaining."],
    ["github_exporter_rate_limited_total", "counter", "kind", "403 responses from GitHub, by which rate limit produced them (primary, secondary, none)."],
  ];

  const pollMetrics = [
    ["github_exporter_poll_duration_seconds", "histogram", "", "Duration of a complete poll cycle in seconds."],
    ["github_exporter_scrape_errors_total", "counter", "collector", "Total scrape errors by collector (repos, workflows, pullrequests, commits, tags, backfill)."],
    ["github_exporter_last_success_timestamp_seconds", "gauge", "collector", "Unix timestamp of last successful scrape by collector."],
  ];

  const requestMetrics = [
    ["github_exporter_api_requests_total", "counter", "activity, kind", "GitHub API requests issued, by activity (poll, backfill) and kind (runs_page, jobs)."],
    ["github_exporter_job_fetches_skipped_total", "counter", "", "Workflow runs whose jobs were already stored and unchanged, so no request was made."],
    ["github_exporter_job_budget_exhausted_total", "counter", "", "Poll cycles that hit their job-request budget and deferred the rest to the backfiller."],
  ];

  const backfillMetrics = [
    ["github_exporter_backfill_pending_job_runs", "gauge", "", "Stored workflow runs still waiting for their jobs to be fetched."],
    ["github_exporter_backfill_repos_complete", "gauge", "", "Repositories whose historical run pagination has reached the end."],
    ["github_exporter_backfill_repos_total", "gauge", "", "Repositories in the backfill rotation."],
    ["github_exporter_backfill_throttled_total", "counter", "reason", "Backfill ticks that deliberately issued no request, by reason."],
    ["github_exporter_backfill_paused", "gauge", "", "1 while the backfiller is holding back to protect the rate limit, 0 otherwise."],
    ["github_exporter_backfill_last_step_timestamp_seconds", "gauge", "", "Unix timestamp of the last backfill tick, whether or not it issued a request."],
  ];

  const renderMetricTable = (metrics: string[][]): string => {
    const rows = metrics
      .map(([name, type, labels, desc]) => {
        const labelsCell = labels ? `<code>${labels}</code>` : "<em>none</em>";
        return `<tr><td><code>${name}</code></td><td>${type}</td><td>${labelsCell}</td><td>${desc}</td></tr>`;
      })
      .join("");
    return `<table><thead><tr><th>Metric</th><th>Type</th><th>Labels</th><th>Description</th></tr></thead><tbody>${rows}</tbody></table>`;
  };

  return `
    <div class="section">
      <h2>GitHub Exporter</h2>
      <p>
        <code>github_exporter</code> polls the GitHub API for CI/CD, pull request,
        commit, and release data across all repositories in a GitHub organization. It
        writes records to PostgreSQL and exposes aggregate Prometheus metrics on a
        standard <code>/metrics</code> endpoint.
      </p>
      <p>
        The exporter authenticates as a <strong>GitHub App</strong> (not a personal access
        token), which gives it a dedicated 5,000 requests/hour rate limit independent of
        any user's quota. Data is persisted in PostgreSQL so that dashboards can query both
        real-time metrics and historical trends spanning months.
      </p>
    </div>

    <div class="section">
      <h2>Architecture</h2>
      <p>Two independent loops divide the collection work:</p>
      <table>
        <thead><tr><th>Subsystem</th><th>Description</th></tr></thead>
        <tbody>
          <tr>
            <td><strong>Poller</strong></td>
            <td>Keeps recent data fresh. Runs every poll interval (default 5 minutes), fetches the first page of workflow runs per repository, and collects pull requests, commits, and tags. Bounded to a per-cycle job budget.</td>
          </tr>
          <tr>
            <td><strong>Backfiller</strong></td>
            <td>Walks history at a bounded request rate. Issues at most one GitHub request every 2 seconds (configurable), may take hours to complete a first fill, and resumes from where it left off after a restart. Round-robins through repositories so every dashboard fills in together.</td>
          </tr>
          <tr>
            <td><strong>Rate Limit Manager</strong></td>
            <td>Classifies every 403 as primary or secondary. The backfiller pauses when primary quota is low; the poller limits per-cycle requests to avoid secondary bursts. Both limits are exposed as metrics.</td>
          </tr>
          <tr>
            <td><strong>PostgreSQL Store</strong></td>
            <td>Persists all collected data with upsert semantics. Statement-level triggers roll up raw data into daily aggregation tables after 90 days and delete the originals. Backfill progress is stored so restarts resume rather than repeat.</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="section">
      <h2>Installation</h2>
      <p>Container images are published to GitHub Container Registry:</p>
      <pre><code>ghcr.io/phaseshiftdata/github_exporter</code></pre>
      <pre><code># Run with a GitHub App and PostgreSQL
docker run -d \\
  --name github_exporter \\
  -p 9102:9102 \\
  -e DATABASE_URL="postgres://user:pass@db:5432/github_exporter" \\
  -v /path/to/key.pem:/etc/github/key.pem:ro \\
  ghcr.io/phaseshiftdata/github_exporter:main \\
  --github-app-id 12345 \\
  --github-install-id 67890 \\
  --github-key-file /etc/github/key.pem \\
  --org your-org \\
  --listen-address 0.0.0.0:9102</code></pre>
    </div>

    <div class="section">
      <h2>Configuration</h2>
      <p>All configuration is via CLI flags. The database URL can also be set via the <code>DATABASE_URL</code> environment variable.</p>
      <table>
        <thead><tr><th>Flag</th><th>Default</th><th>Description</th></tr></thead>
        <tbody>${configTable}</tbody>
      </table>
    </div>

    <div class="section">
      <h2>Secret Configuration</h2>
      <p>
        The database password can be provided in three mutually exclusive ways.
        Setting more than one source for the same secret is a configuration error.
      </p>

      <h3>Embedded in Connection String</h3>
      <p>
        Include the password directly in the <code>--database-url</code> value
        (e.g., <code>postgres://user:pass@host/db</code>). This is the simplest
        approach but the value is visible in process listings.
      </p>

      <h3>File-Based Secrets</h3>
      <p>
        Use <code>--database-password-file</code> to read the password from a
        file at startup. Trailing whitespace and newlines are trimmed. The
        password is substituted into the <code>--database-url</code> connection
        string.
      </p>

      <h3>OpenBao-Backed Secrets</h3>
      <p>
        For deployments using <a href="https://openbao.org/">OpenBao</a> (or
        HashiCorp Vault), use <code>--database-password-openbao</code> to read
        the password from a KV v2 secret engine. This avoids placing secrets on
        disk entirely.
      </p>
      <table>
        <thead><tr><th>Flag</th><th>Description</th></tr></thead>
        <tbody>
          <tr><td><code>--openbao-address</code></td><td>OpenBao/Vault server address (env: <code>OPENBAO_ADDR</code>).</td></tr>
          <tr><td><code>--openbao-approle-role-id-file</code></td><td>Path to a file containing the AppRole <code>role_id</code>.</td></tr>
          <tr><td><code>--openbao-approle-secret-id-file</code></td><td>Path to a file containing the AppRole <code>secret_id</code>.</td></tr>
          <tr><td><code>--database-password-openbao=&lt;path&gt;:&lt;field&gt;</code></td><td>Read the database password from the given KV v2 path and field.</td></tr>
        </tbody>
      </table>

      <h3>Summary</h3>
      <table>
        <thead><tr><th>Credential</th><th>Flag</th><th>File</th><th>OpenBao</th></tr></thead>
        <tbody>
          <tr>
            <td>Database password</td>
            <td>Embedded in <code>--database-url</code></td>
            <td><code>--database-password-file</code></td>
            <td><code>--database-password-openbao</code></td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="section">
      <h2>GitHub App Setup</h2>
      <p>The exporter authenticates as a <strong>GitHub App installation</strong>, which provides a dedicated rate limit of 5,000 requests per hour.</p>
      <table>
        <thead><tr><th>Permission</th><th>Access</th><th>Needed For</th></tr></thead>
        <tbody>
          <tr><td>Repository &rarr; Actions</td><td>Read</td><td>Workflow runs and jobs</td></tr>
          <tr><td>Repository &rarr; Contents</td><td>Read</td><td>Commits and tags</td></tr>
          <tr><td>Repository &rarr; Metadata</td><td>Read</td><td>Repository discovery (granted by default)</td></tr>
          <tr><td>Repository &rarr; Pull requests</td><td>Read</td><td>Pull request data</td></tr>
        </tbody>
      </table>
      <p>
        Create the app under your organization's <strong>Settings &gt; Developer settings &gt;
        GitHub Apps</strong>. Disable webhooks (the exporter polls). Generate a private key and
        install the app on the organization. Pass the App ID, Installation ID, and private key
        path via CLI flags.
      </p>
    </div>

    <div class="section">
      <h2>Database</h2>
      <p>
        The exporter requires PostgreSQL 14 or later. It runs schema migrations automatically
        on startup. A dedicated database (e.g., <code>github_exporter</code>) is recommended.
      </p>
      <h3>Schema</h3>
      <p>
        <strong>Raw event tables</strong> (pruned after 90 days, rolled up before deletion):
        repositories, workflow_runs, workflow_jobs, pull_requests, commits, tags.
      </p>
      <p>
        <strong>Daily rollup tables</strong> (retained indefinitely): workflow_runs_daily,
        workflow_jobs_daily, pull_requests_daily, commits_daily. These hold counts,
        pass/fail breakdowns, and percentile durations for CI/CD trends.
      </p>
      <p>
        <strong>Operational tables</strong>: backfill_progress (per-repository backfill
        state), schema_migrations (applied migration tracking).
      </p>
      <h3>Retention</h3>
      <p>
        Raw event data is retained for 90 days. Statement-level triggers on INSERT roll up
        expiring rows into daily tables and delete them. Tags are exempt from pruning (current
        state, not events). Open pull requests are never pruned regardless of age.
      </p>
    </div>

    <div class="section">
      <h2>Metrics: Repository and CI/CD</h2>
      ${renderMetricTable(cicdMetrics)}
    </div>

    <div class="section">
      <h2>Metrics: Rate Limit</h2>
      ${renderMetricTable(rateLimitMetrics)}
    </div>

    <div class="section">
      <h2>Metrics: Poll Cycle</h2>
      ${renderMetricTable(pollMetrics)}
    </div>

    <div class="section">
      <h2>Metrics: Request Accounting</h2>
      ${renderMetricTable(requestMetrics)}
    </div>

    <div class="section">
      <h2>Metrics: Backfill</h2>
      ${renderMetricTable(backfillMetrics)}
    </div>

    <div class="section">
      <h2>Rate Limiting</h2>
      <p>
        GitHub enforces two distinct rate limits, and the exporter handles them differently:
      </p>
      <table>
        <thead><tr><th>Limit</th><th>What It Caps</th><th>Exporter Defense</th></tr></thead>
        <tbody>
          <tr>
            <td><strong>Primary</strong></td>
            <td>5,000 requests per hour for GitHub App installations.</td>
            <td>Backfiller pauses when remaining quota falls below <code>--backfill-min-rate-limit</code>. Poller is bounded by design.</td>
          </tr>
          <tr>
            <td><strong>Secondary</strong></td>
            <td>Request burst rate. Triggers 403 regardless of remaining quota.</td>
            <td>Per-cycle job budget (<code>--job-budget-per-poll</code>), fixed-interval backfill spacing, one page of runs per repo per cycle.</td>
          </tr>
        </tbody>
      </table>
      <p>
        A cold start with 25 repositories takes a few hours to fully populate. This is the
        intended behavior: an exporter that fills in visibly over hours is worth more than
        one that completes in ten minutes and then wedges for the rest of the day.
      </p>
    </div>

    <div class="section">
      <h2>Failure Modes</h2>
      <table>
        <thead><tr><th>Scenario</th><th>Behavior</th></tr></thead>
        <tbody>
          <tr>
            <td><strong>Secondary rate limit (403)</strong></td>
            <td>Client classifies the limit, backs off (60s default for secondary), retries up to 3 times, then returns an error. The poller moves to the next repository; the backfiller retries on the next tick.</td>
          </tr>
          <tr>
            <td><strong>Primary rate limit exhaustion</strong></td>
            <td>Client sleeps until the reset timestamp (capped at 2 minutes). Backfiller pauses when remaining quota is low. Metrics remain served at their last-known values.</td>
          </tr>
          <tr>
            <td><strong>Database unavailable</strong></td>
            <td>Errors are logged and <code>github_exporter_scrape_errors_total</code> incremented. Individual collector failures do not halt the poll cycle.</td>
          </tr>
          <tr>
            <td><strong>Authentication failure</strong></td>
            <td>Invalid private key or installation token errors cause the exporter to exit on startup. Token refresh is automatic (5 minutes before expiry).</td>
          </tr>
          <tr>
            <td><strong>No database configured</strong></td>
            <td>Runs in metrics-only mode: HTTP server starts but no polling or database writes occur.</td>
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
          <tr><td><code>/</code></td><td>GET</td><td>Landing page with a link to <code>/metrics</code>.</td></tr>
        </tbody>
      </table>
    </div>

    <div class="section">
      <h2>Deployment</h2>
      <h3>Kubernetes</h3>
      <pre><code>apiVersion: apps/v1
kind: Deployment
metadata:
  name: github-exporter
spec:
  replicas: 1
  selector:
    matchLabels:
      app: github-exporter
  template:
    metadata:
      labels:
        app: github-exporter
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9102"
    spec:
      containers:
        - name: github-exporter
          image: ghcr.io/phaseshiftdata/github_exporter:main
          args:
            - --github-app-id=12345
            - --github-install-id=67890
            - --github-key-file=/etc/github/key.pem
            - --org=your-org
            - --listen-address=0.0.0.0:9102
          ports:
            - containerPort: 9102
          env:
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: github-exporter
                  key: database-url
          volumeMounts:
            - name: github-key
              mountPath: /etc/github
              readOnly: true
          livenessProbe:
            httpGet:
              path: /
              port: 9102
          readinessProbe:
            httpGet:
              path: /
              port: 9102</code></pre>

      <h3>Prometheus Scrape Config</h3>
      <pre><code>scrape_configs:
  - job_name: github
    scrape_interval: 60s
    static_configs:
      - targets:
          - github-exporter:9102</code></pre>
      <p>The scrape interval can be shorter than the poll interval. Between polls, Prometheus receives the same metric values.</p>

      <h3>Single-Replica Topology</h3>
      <p>
        The exporter is designed to run as a <strong>single replica</strong>. Running multiple
        replicas against the same GitHub App installation multiplies API call volume and may
        exhaust the shared rate limit. If high availability is required, run one active replica
        with a standby that only starts if the active fails.
      </p>
    </div>`;
}
