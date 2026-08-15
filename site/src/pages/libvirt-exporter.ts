export function LibvirtExporterPage(): string {
  const configRows = [
    ["--listen-address", "127.0.0.1:9177", "Address and port the HTTP server listens on."],
    ["--libvirt-uri", "qemu:///system", "Libvirt connection URI. Use <code>qemu:///system</code> for the system daemon or <code>qemu+tcp://host/system</code> for remote TCP connections."],
    ["--log-level", "info", "Log verbosity. One of <code>debug</code>, <code>info</code>, <code>warn</code>, <code>error</code>."],
  ];

  const configTable = configRows
    .map(([flag, def, desc]) => `<tr><td><code>${flag}</code></td><td><code>${def}</code></td><td>${desc}</td></tr>`)
    .join("");

  const domainStates = [
    ["1", "RUNNING", "The domain is actively running."],
    ["2", "BLOCKED", "The domain is blocked on a resource (waiting for I/O)."],
    ["3", "PAUSED", "The domain has been paused by the user."],
    ["4", "SHUTDOWN", "The domain is in the process of shutting down."],
    ["5", "SHUTOFF", "The domain is shut off (not running)."],
    ["6", "CRASHED", "The domain has crashed."],
    ["7", "SUSPENDED", "The domain is suspended (saved to disk)."],
  ];

  const hypervisorMetrics = [
    ["libvirt_up", "gauge", "", "Whether the libvirt daemon is reachable (1 = up, 0 = down)."],
    ["libvirt_domains_total", "gauge", "", "Total number of defined domains (VMs)."],
    ["libvirt_host_cpu_count", "gauge", "", "Number of physical CPUs on the hypervisor host."],
    ["libvirt_host_memory_bytes", "gauge", "", "Total physical memory on the hypervisor host in bytes."],
    ["libvirt_host_free_memory_bytes", "gauge", "", "Free physical memory on the hypervisor host in bytes."],
  ];

  const domainInfoMetrics = [
    ["libvirt_domain_info_state", "gauge", "domain, uuid", "Numeric domain state (see Domain States table)."],
    ["libvirt_domain_info_max_memory_bytes", "gauge", "domain, uuid", "Maximum allowed memory for the domain in bytes."],
    ["libvirt_domain_info_memory_bytes", "gauge", "domain, uuid", "Current memory allocation for the domain in bytes."],
    ["libvirt_domain_info_vcpus", "gauge", "domain, uuid", "Number of virtual CPUs assigned to the domain."],
    ["libvirt_domain_cpu_time_seconds_total", "counter", "domain, uuid", "Total CPU time consumed by the domain in seconds."],
  ];

  const memoryMetrics = [
    ["libvirt_domain_memory_stats_bytes", "gauge", "domain, uuid, stat", "Memory statistics for the domain. The <code>stat</code> label identifies the statistic (e.g. <code>actual</code>, <code>rss</code>, <code>unused</code>, <code>available</code>, <code>usable</code>, <code>swap_in</code>, <code>swap_out</code>)."],
  ];

  const blockMetrics = [
    ["libvirt_domain_block_read_bytes_total", "counter", "domain, uuid, device", "Total bytes read from the block device."],
    ["libvirt_domain_block_read_requests_total", "counter", "domain, uuid, device", "Total read requests issued to the block device."],
    ["libvirt_domain_block_write_bytes_total", "counter", "domain, uuid, device", "Total bytes written to the block device."],
    ["libvirt_domain_block_write_requests_total", "counter", "domain, uuid, device", "Total write requests issued to the block device."],
  ];

  const netMetrics = [
    ["libvirt_domain_net_receive_bytes_total", "counter", "domain, uuid, interface", "Total bytes received on the network interface."],
    ["libvirt_domain_net_receive_packets_total", "counter", "domain, uuid, interface", "Total packets received on the network interface."],
    ["libvirt_domain_net_receive_errors_total", "counter", "domain, uuid, interface", "Total receive errors on the network interface."],
    ["libvirt_domain_net_transmit_bytes_total", "counter", "domain, uuid, interface", "Total bytes transmitted on the network interface."],
    ["libvirt_domain_net_transmit_packets_total", "counter", "domain, uuid, interface", "Total packets transmitted on the network interface."],
    ["libvirt_domain_net_transmit_errors_total", "counter", "domain, uuid, interface", "Total transmit errors on the network interface."],
  ];

  const renderMetricTable = (metrics: string[][]): string => {
    const rows = metrics
      .map(([name, type, labels, desc]) => {
        const labelCell = labels ? `<code>${labels}</code>` : "";
        return `<tr><td><code>${name}</code></td><td>${type}</td><td>${labelCell}</td><td>${desc}</td></tr>`;
      })
      .join("");
    return `<table><thead><tr><th>Metric</th><th>Type</th><th>Labels</th><th>Description</th></tr></thead><tbody>${rows}</tbody></table>`;
  };

  const renderStateTable = (states: string[][]): string => {
    const rows = states
      .map(([val, name, desc]) => `<tr><td><code>${val}</code></td><td>${name}</td><td>${desc}</td></tr>`)
      .join("");
    return `<table><thead><tr><th>Value</th><th>State</th><th>Description</th></tr></thead><tbody>${rows}</tbody></table>`;
  };

  return `
    <div class="section">
      <h2>Libvirt Exporter</h2>
      <p>
        <code>libvirt_exporter</code> is a Prometheus exporter for libvirtd. It collects
        hypervisor-level and per-virtual-machine metrics via the libvirt API, covering CPU,
        memory, block I/O, and network I/O for every domain managed by the hypervisor.
      </p>
      <p>
        The exporter connects to the libvirt daemon on every scrape and enumerates all domains
        (VMs). Domains are auto-discovered; no static configuration of VM names or UUIDs is
        required.
      </p>
    </div>

    <div class="section">
      <h2>Architecture</h2>
      <p>
        The exporter registers a single collector into a Prometheus registry. The collector
        connects to libvirtd via its Unix socket (or optionally TCP) using Go libvirt bindings
        that require CGO. On each scrape the collector:
      </p>
      <ol>
        <li>Connects to the libvirt daemon.</li>
        <li>Queries hypervisor-level information (host CPU count, total memory, free memory).</li>
        <li>Lists all domains and collects per-domain info, memory stats, block device stats, and network interface stats.</li>
        <li>Closes the connection.</li>
      </ol>
      <p>
        The connection is not held open between scrapes. Each scrape creates a fresh libvirt
        session, ensuring the exporter does not accumulate stale state.
      </p>
      <p>
        The HTTP handler is configured with <code>ContinueOnError</code> so that a failure
        collecting statistics for an individual domain does not prevent metrics for the
        remaining domains from being returned.
      </p>
    </div>

    <div class="section">
      <h2>Installation</h2>
      <p>Container images are published to GitHub Container Registry:</p>
      <pre><code>ghcr.io/phaseshiftdata/libvirt_exporter</code></pre>
      <pre><code># Run on a hypervisor host
docker run -d \\
  --name libvirt_exporter \\
  -v /var/run/libvirt/libvirt-sock:/var/run/libvirt/libvirt-sock:ro \\
  -p 9177:9177 \\
  ghcr.io/phaseshiftdata/libvirt_exporter:main \\
  --listen-address=0.0.0.0:9177</code></pre>
      <p>
        The container requires access to the host's libvirt Unix socket. The libvirtd service
        must be running and the socket must be accessible by the container's user.
      </p>
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
      <h2>Metrics: Hypervisor</h2>
      <p>
        Hypervisor-level metrics report the overall state of the libvirt host, including
        connectivity, domain count, CPU count, and memory.
      </p>
      ${renderMetricTable(hypervisorMetrics)}
    </div>

    <div class="section">
      <h2>Metrics: Domain Info</h2>
      <p>
        Per-domain metrics carry the labels <code>domain</code> (VM name) and <code>uuid</code>
        (libvirt UUID). Domain info metrics report the state, memory allocation, vCPU count,
        and cumulative CPU time for each domain.
      </p>
      ${renderMetricTable(domainInfoMetrics)}
    </div>

    <div class="section">
      <h2>Metrics: Memory Statistics</h2>
      <p>
        Memory statistics are reported per domain with a <code>stat</code> label identifying
        the specific statistic. The available statistics depend on the guest agent and
        hypervisor capabilities.
      </p>
      ${renderMetricTable(memoryMetrics)}
    </div>

    <div class="section">
      <h2>Metrics: Block Devices</h2>
      <p>
        Block device metrics report read and write byte and request counters per device.
        The <code>device</code> label identifies the block device (e.g. <code>vda</code>,
        <code>sda</code>).
      </p>
      ${renderMetricTable(blockMetrics)}
    </div>

    <div class="section">
      <h2>Metrics: Network Interfaces</h2>
      <p>
        Network interface metrics report receive and transmit byte, packet, and error counters.
        The <code>interface</code> label identifies the virtual network interface (e.g.
        <code>vnet0</code>, <code>vnet1</code>).
      </p>
      ${renderMetricTable(netMetrics)}
    </div>

    <div class="section">
      <h2>Domain States</h2>
      <p>The <code>libvirt_domain_info_state</code> metric reports a numeric state value corresponding to the libvirt domain lifecycle:</p>
      ${renderStateTable(domainStates)}
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
      <h3>Host Requirements</h3>
      <table>
        <thead><tr><th>Resource</th><th>Mount</th><th>Purpose</th></tr></thead>
        <tbody>
          <tr><td>libvirt socket</td><td><code>/var/run/libvirt/libvirt-sock</code></td><td>Domain enumeration and metric collection</td></tr>
        </tbody>
      </table>
      <p>
        The libvirtd service must be running on the host. If the socket is permission-restricted,
        run the container as root or ensure the container user belongs to the <code>libvirt</code>
        group on the host.
      </p>

      <h3>Kubernetes DaemonSet</h3>
      <pre><code>apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: libvirt-exporter
spec:
  selector:
    matchLabels:
      app: libvirt-exporter
  template:
    metadata:
      labels:
        app: libvirt-exporter
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9177"
    spec:
      nodeSelector:
        node-role.kubernetes.io/hypervisor: ""
      containers:
        - name: libvirt-exporter
          image: ghcr.io/phaseshiftdata/libvirt_exporter:main
          args:
            - --listen-address=0.0.0.0:9177
          ports:
            - containerPort: 9177
              hostPort: 9177
          volumeMounts:
            - name: libvirt-sock
              mountPath: /var/run/libvirt/libvirt-sock
              readOnly: true
      volumes:
        - name: libvirt-sock
          hostPath:
            path: /var/run/libvirt/libvirt-sock
            type: Socket</code></pre>

      <h3>Prometheus Scrape Config</h3>
      <pre><code>scrape_configs:
  - job_name: libvirt
    scrape_interval: 30s
    static_configs:
      - targets:
          - hypervisor-1:9177
          - hypervisor-2:9177</code></pre>

      <h3>Docker Compose</h3>
      <pre><code>services:
  libvirt-exporter:
    image: ghcr.io/phaseshiftdata/libvirt_exporter:main
    command:
      - --listen-address=0.0.0.0:9177
    ports:
      - "9177:9177"
    volumes:
      - /var/run/libvirt/libvirt-sock:/var/run/libvirt/libvirt-sock:ro
    restart: unless-stopped</code></pre>
    </div>

    <div class="section">
      <h2>Failure Modes</h2>
      <table>
        <thead><tr><th>Scenario</th><th>Behavior</th></tr></thead>
        <tbody>
          <tr>
            <td><strong>libvirtd unreachable</strong></td>
            <td>The collector emits <code>libvirt_up 0</code> and no other <code>libvirt_*</code> metrics. The scrape still returns HTTP 200.</td>
          </tr>
          <tr>
            <td><strong>Individual domain stat failure</strong></td>
            <td>The <code>ContinueOnError</code> handler ensures that a failure collecting stats for one domain (e.g., a domain destroyed mid-scrape) does not prevent metrics for other domains from being returned.</td>
          </tr>
          <tr>
            <td><strong>Socket permissions</strong></td>
            <td>If the exporter process cannot open the libvirt socket due to file permissions, the collector emits <code>libvirt_up 0</code>. Run the container as root or ensure the container user belongs to the <code>libvirt</code> group.</td>
          </tr>
          <tr>
            <td><strong>Domain destroyed mid-scrape</strong></td>
            <td>If a domain is destroyed between the list call and the stats call, the stats call for that domain fails gracefully. Metrics for all other domains are unaffected.</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="section">
      <h2>CGO Note</h2>
      <p>
        Unlike the other exporters in this repository, <code>libvirt_exporter</code> requires
        CGO because the Go libvirt bindings link against the native <code>libvirt-dev</code>
        C library. This means:
      </p>
      <ul>
        <li>The container image cannot use a distroless or scratch base image. It uses a minimal base that includes the <code>libvirt</code> shared libraries.</li>
        <li>Cross-compilation requires a C cross-compiler and the target platform's <code>libvirt-dev</code> headers.</li>
        <li>The build stage in the Dockerfile installs <code>libvirt-dev</code> before running <code>go build</code>.</li>
      </ul>
    </div>`;
}
