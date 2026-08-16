export function NetworkExporterPage(): string {
  const configRows = [
    ["--listen-address", "127.0.0.1:9100", "Address and port the HTTP server listens on."],
    ["--proc-path", "/proc", "Path to procfs mount. Set to <code>/host/proc</code> when running in a container with the host's procfs bind-mounted."],
    ["--sys-path", "/sys", "Path to sysfs mount. Set to <code>/host/sys</code> when running in a container with the host's sysfs bind-mounted."],
    ["--log-level", "info", "Log verbosity. One of <code>debug</code>, <code>info</code>, <code>warn</code>, <code>error</code>."],
  ];

  const configTable = configRows
    .map(([flag, def, desc]) => `<tr><td><code>${flag}</code></td><td><code>${def}</code></td><td>${desc}</td></tr>`)
    .join("");

  const arpMetrics = [
    ["network_arp_entry", "gauge", "ip, mac, device, state", "ARP table entry; value is always 1."],
  ];

  const ifaceMetrics = [
    ["network_interface_type", "gauge", "device, type, driver", "Interface type classification; value is always 1."],
    ["network_bond_member", "gauge", "bond, member", "Bond membership; value is always 1."],
    ["network_bridge_member", "gauge", "bridge, member", "Bridge membership; value is always 1."],
  ];

  const netgraphMetrics = [
    ["network_graph_edge", "gauge", "remote_host, local_port, direction", "Presence indicator for a network topology edge; value is always 1."],
  ];

  const conntrackMetrics = [
    ["network_port_connections", "gauge", "port, protocol, state", "Number of connections per port, protocol, and state."],
    ["network_port_listen", "gauge", "port, protocol, bind_address", "Presence of a listening port; value is always 1."],
    ["network_port_bytes_in", "gauge", "port, protocol", "Total inbound bytes per port from conntrack."],
    ["network_port_bytes_out", "gauge", "port, protocol", "Total outbound bytes per port from conntrack."],
    ["network_conntrack_accounting_enabled", "gauge", "<em>(none)</em>", "Whether conntrack accounting is available (1) or not (0)."],
  ];

  const firewallMetrics = [
    ["network_firewall_collector_up", "gauge", "<em>(none)</em>", "Whether nftables counters could be read (1 = collecting, 0 = unavailable)."],
    ["network_firewall_drop_packets_total", "counter", "family, table, chain, rule", "Total packets dropped by nftables DROP rules."],
    ["network_firewall_drop_bytes_total", "counter", "family, table, chain, rule", "Total bytes dropped by nftables DROP rules."],
    ["network_firewall_reject_packets_total", "counter", "family, table, chain, rule", "Total packets rejected by nftables REJECT rules."],
    ["network_firewall_reject_bytes_total", "counter", "family, table, chain, rule", "Total bytes rejected by nftables REJECT rules."],
    ["network_firewall_policy_drop_packets_total", "counter", "family, table, chain", "Total packets dropped by chain default DROP policy."],
    ["network_firewall_policy_drop_bytes_total", "counter", "family, table, chain", "Total bytes dropped by chain default DROP policy."],
  ];

  const renderMetricTable = (metrics: string[][]): string => {
    const rows = metrics
      .map(([name, type, labels, desc]) => `<tr><td><code>${name}</code></td><td>${type}</td><td><code>${labels}</code></td><td>${desc}</td></tr>`)
      .join("");
    return `<table><thead><tr><th>Metric</th><th>Type</th><th>Labels</th><th>Description</th></tr></thead><tbody>${rows}</tbody></table>`;
  };

  return `
    <div class="section">
      <h2>Network Exporter</h2>
      <p>
        <code>network_exporter</code> collects host-level network metrics from
        procfs, sysfs, and kernel netlink sockets and presents them on a standard
        <code>/metrics</code> endpoint. It is designed to <strong>complement, not
        replace</strong> Alloy's embedded <code>node_exporter</code>: it exposes
        the subset of network telemetry that <code>node_exporter</code> does not
        cover &mdash; interface classification, ARP table state, per-port connection
        visibility, network topology graphs, and firewall drop/reject counters.
      </p>
      <p>
        The exporter runs as a single container per host. It reads directly from
        kernel interfaces, requiring no external binaries or shell access. The
        runtime image is distroless.
      </p>
    </div>

    <div class="section">
      <h2>Architecture</h2>
      <p>
        The exporter follows a pull-based architecture. Each Prometheus scrape
        triggers all registered collectors, which read live kernel state and return
        metrics for that instant. There is no internal caching or aggregation; every
        scrape reflects the current state of the host.
      </p>
      <table>
        <thead><tr><th>Collector</th><th>Data Source</th><th>Description</th></tr></thead>
        <tbody>
          <tr>
            <td><strong>arp</strong></td>
            <td>Netlink (<code>NETLINK_ROUTE</code>)</td>
            <td>Full IPv4 ARP neighbor table with NUD state.</td>
          </tr>
          <tr>
            <td><strong>iface</strong></td>
            <td>sysfs (<code>/sys/class/net/</code>)</td>
            <td>Interface type classification, bond membership, bridge membership.</td>
          </tr>
          <tr>
            <td><strong>netgraph</strong></td>
            <td>procfs (<code>/proc/net/tcp</code>, <code>/proc/net/udp</code>)</td>
            <td>Deduplicated network topology edges between local listening ports and remote hosts.</td>
          </tr>
          <tr>
            <td><strong>conntrack</strong></td>
            <td>procfs + Netlink (conntrack subsystem)</td>
            <td>Per-port connection counts by state, listening port presence, and per-port byte counters.</td>
          </tr>
          <tr>
            <td><strong>firewall</strong></td>
            <td>Netlink (nf_tables subsystem)</td>
            <td>Packet and byte counters for nftables DROP/REJECT rules and chain default DROP policies.</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="section">
      <h2>Installation</h2>
      <p>Container images are published to GitHub Container Registry:</p>
      <pre><code>ghcr.io/phaseshiftdata/network_exporter</code></pre>
      <pre><code># Run with host procfs/sysfs access
docker run -d --rm \\
  --name network_exporter \\
  --network=host --pid=host \\
  --user 0:0 \\
  --cap-add=NET_ADMIN \\
  --cap-add=DAC_READ_SEARCH \\
  --security-opt label=disable \\
  -v /proc:/host/proc:ro \\
  -v /sys:/host/sys:ro \\
  ghcr.io/phaseshiftdata/network_exporter:main \\
  --proc-path=/host/proc \\
  --sys-path=/host/sys \\
  --listen-address=0.0.0.0:9100</code></pre>

      <h3>Host Requirements</h3>
      <p>
        The exporter must run as root (<code>--user 0:0</code>) because reading
        procfs entries of other processes and querying nftables counters require
        root privileges. The default distroless UID 65532 is not sufficient.
      </p>
      <table>
        <thead><tr><th>Requirement</th><th>Reason</th></tr></thead>
        <tbody>
          <tr><td><strong>procfs mount</strong> (<code>/proc</code> &rarr; <code>/host/proc</code>)</td><td>The netgraph and conntrack collectors parse <code>/proc/net/tcp</code> and <code>/proc/net/udp</code> to discover connections.</td></tr>
          <tr><td><strong>sysfs mount</strong> (<code>/sys</code> &rarr; <code>/host/sys</code>)</td><td>The iface collector reads <code>/sys/class/net/</code> to classify interfaces.</td></tr>
          <tr><td><strong>Host PID namespace</strong> (<code>--pid=host</code>)</td><td>Required to see the host's <code>/proc/net/</code> rather than the container's.</td></tr>
          <tr><td><strong>Host network namespace</strong> (<code>--network=host</code>)</td><td>Required so netlink sockets operate in the host's network namespace.</td></tr>
          <tr><td><strong>Root user</strong> (<code>--user 0:0</code>)</td><td>Required for reading procfs entries of other processes and querying nftables counters.</td></tr>
          <tr><td><strong><code>CAP_NET_ADMIN</code></strong></td><td>Required by <code>NETLINK_NETFILTER</code> for conntrack flow queries and nftables rule/counter reads.</td></tr>
          <tr><td><strong><code>CAP_DAC_READ_SEARCH</code></strong></td><td>Required for reading <code>/proc</code> entries of other processes.</td></tr>
          <tr><td><strong><code>--security-opt label=disable</code></strong></td><td>Required on SELinux hosts to allow the container to read host procfs and sysfs mounts.</td></tr>
        </tbody>
      </table>
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
      <h2>Metrics: ARP</h2>
      <p>
        Reads the full IPv4 neighbor table via netlink and emits one gauge per entry.
        The <code>state</code> label carries the kernel NUD state: <code>incomplete</code>,
        <code>reachable</code>, <code>stale</code>, <code>delay</code>, <code>probe</code>,
        <code>failed</code>, <code>noarp</code>, or <code>permanent</code>.
      </p>
      ${renderMetricTable(arpMetrics)}
    </div>

    <div class="section">
      <h2>Metrics: Interface</h2>
      <p>
        Classifies all network interfaces by type using sysfs attributes and reports
        bond and bridge membership relationships. The <code>type</code> label is one
        of <code>physical</code>, <code>bond</code>, <code>bridge</code>,
        <code>veth</code>, <code>vti</code>, <code>loopback</code>, or
        <code>other</code>. The <code>driver</code> label is read from the sysfs
        <code>device/driver</code> symlink (e.g. <code>ixgbe</code>, <code>bridge</code>).
      </p>
      ${renderMetricTable(ifaceMetrics)}
    </div>

    <div class="section">
      <h2>Metrics: Network Graph</h2>
      <p>
        Discovers network topology edges by examining active TCP and UDP connections
        relative to local listening ports. Edges are deduplicated by
        <code>(remote_host, local_port, direction)</code>, keeping cardinality
        proportional to distinct service relationships rather than individual connection
        count. The <code>direction</code> label is <code>inbound</code> when a remote
        host connects to a local listening port, or <code>outbound</code> when this
        host connects to a remote service.
      </p>
      ${renderMetricTable(netgraphMetrics)}
    </div>

    <div class="section">
      <h2>Metrics: Conntrack</h2>
      <p>
        Reports per-port connection counts by state, listening port presence, and
        per-port byte counters from the kernel's connection tracking subsystem.
        Socket data comes from procfs; flow accounting data comes from
        <code>NETLINK_NETFILTER</code>. When conntrack accounting is unavailable,
        <code>network_conntrack_accounting_enabled</code> reads <code>0</code> and
        byte counter metrics are omitted.
      </p>
      ${renderMetricTable(conntrackMetrics)}
    </div>

    <div class="section">
      <h2>Metrics: Firewall</h2>
      <p>
        Reports packet and byte counters for nftables DROP and REJECT rules, plus
        chain default DROP policies. The collector reads nftables over the kernel's
        netlink interface (via <code>github.com/google/nftables</code>), requiring no
        <code>nft</code> binary in the container image. On startup, the collector probes
        whether it can access <code>NETLINK_NETFILTER</code>; if not (missing
        <code>CAP_NET_ADMIN</code> or no nf_tables in the kernel), it latches to a
        permanently-down state and reports <code>network_firewall_collector_up 0</code>
        on every scrape.
      </p>
      ${renderMetricTable(firewallMetrics)}
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
      <h3>Kubernetes DaemonSet</h3>
      <p>
        The exporter should run as a DaemonSet so every node in the cluster is
        monitored. It requires host PID and network namespaces, plus procfs and
        sysfs bind mounts.
      </p>
      <pre><code>apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: network-exporter
spec:
  selector:
    matchLabels:
      app: network-exporter
  template:
    metadata:
      labels:
        app: network-exporter
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9100"
    spec:
      hostPID: true
      hostNetwork: true
      containers:
        - name: network-exporter
          image: ghcr.io/phaseshiftdata/network_exporter:main
          args:
            - --proc-path=/host/proc
            - --sys-path=/host/sys
            - --listen-address=0.0.0.0:9100
          ports:
            - containerPort: 9100
              hostPort: 9100
          securityContext:
            runAsUser: 0
            runAsGroup: 0
            capabilities:
              add:
                - NET_ADMIN
                - DAC_READ_SEARCH
            seLinuxOptions:
              type: spc_t
          volumeMounts:
            - name: proc
              mountPath: /host/proc
              readOnly: true
            - name: sys
              mountPath: /host/sys
              readOnly: true
      volumes:
        - name: proc
          hostPath:
            path: /proc
        - name: sys
          hostPath:
            path: /sys</code></pre>

      <h3>Prometheus Scrape Config</h3>
      <pre><code>scrape_configs:
  - job_name: network
    scrape_interval: 30s
    static_configs:
      - targets:
          - network-exporter:9100</code></pre>
    </div>

    <div class="section">
      <h2>Failure Modes</h2>
      <table>
        <thead><tr><th>Scenario</th><th>Behavior</th></tr></thead>
        <tbody>
          <tr>
            <td><strong>Single collector failure</strong></td>
            <td>The <code>/metrics</code> endpoint uses <code>ContinueOnError</code>. When one collector cannot read its data source, the remaining collectors still contribute their metrics. The failed collector's metric families are absent from that scrape rather than the entire response returning HTTP 500.</td>
          </tr>
          <tr>
            <td><strong>Missing procfs</strong></td>
            <td>The netgraph and conntrack collectors fail to parse <code>/proc/net/tcp</code>. Their metric families are absent, but ARP, interface, and firewall collectors continue.</td>
          </tr>
          <tr>
            <td><strong>Missing sysfs</strong></td>
            <td>The interface collector fails to read <code>/sys/class/net/</code>. Its metrics (<code>network_interface_type</code>, <code>network_bond_member</code>, <code>network_bridge_member</code>) are absent, but all other collectors continue.</td>
          </tr>
          <tr>
            <td><strong>nftables not available</strong></td>
            <td>The firewall collector latches to a permanently-down state at startup. Every scrape reports <code>network_firewall_collector_up 0</code> and omits drop/reject/policy counters. This is logged once and does not produce repeated log entries.</td>
          </tr>
          <tr>
            <td><strong>Conntrack accounting disabled</strong></td>
            <td>The collector reports <code>network_conntrack_accounting_enabled 0</code> and omits byte counter metrics. Socket-based metrics (<code>network_port_connections</code>, <code>network_port_listen</code>) continue because they read from procfs.</td>
          </tr>
        </tbody>
      </table>
    </div>`;
}
