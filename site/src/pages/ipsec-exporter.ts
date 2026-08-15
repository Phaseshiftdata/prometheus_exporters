export function IpsecExporterPage(): string {
  const configRows = [
    ["--listen-address", "127.0.0.1:9100", "Address and port the HTTP server listens on."],
    ["--proc-path", "/proc", "Path to the procfs mount point. Override when running in a container with procfs bind-mounted to a non-default path."],
    ["--sys-path", "/sys", "Path to the sysfs mount point. Override when running in a container with sysfs bind-mounted to a non-default path."],
    ["--vici-socket", "/var/run/charon.vici", "Path to the strongSwan VICI Unix socket."],
    ["--log-level", "info", "Log verbosity. One of <code>debug</code>, <code>info</code>, <code>warn</code>, <code>error</code>."],
  ];

  const configTable = configRows
    .map(([flag, def, desc]) => `<tr><td><code>${flag}</code></td><td><code>${def}</code></td><td>${desc}</td></tr>`)
    .join("");

  const ikeStates = [
    ["0", "CREATED", "SA allocated but not yet negotiating."],
    ["1", "CONNECTING", "IKE negotiation in progress."],
    ["2", "ESTABLISHED", "SA is up and operational."],
    ["3", "PASSIVE", "Passive SA (responder waiting)."],
    ["4", "REKEYING", "Rekey in progress."],
    ["5", "REKEYED", "Old SA after a successful rekey (being replaced)."],
    ["6", "DELETING", "SA is being torn down."],
    ["7", "DESTROYING", "SA is being destroyed."],
  ];

  const childStates = [
    ["0", "CREATED", "Child SA allocated."],
    ["1", "ROUTED", "Trap policy installed, waiting for traffic."],
    ["2", "INSTALLING", "SPIs allocated, installing policies."],
    ["3", "INSTALLED", "SA is installed and passing traffic."],
    ["4", "UPDATING", "SA parameters are being updated."],
    ["5", "REKEYING", "Rekey in progress."],
    ["6", "REKEYED", "Old child SA after a successful rekey."],
    ["7", "RETRYING", "Retrying negotiation after a failure."],
    ["8", "DELETING", "Child SA is being deleted."],
    ["9", "DELETED", "Child SA has been deleted."],
    ["10", "DESTROYING", "Child SA is being destroyed."],
  ];

  const ipsecMetrics = [
    ["ipsec_up", "gauge", "", "Whether the VICI socket is reachable (1 = up, 0 = down)."],
    ["ipsec_ike_sas", "gauge", "", "Total number of IKE SAs."],
    ["ipsec_half_open_ike_sas", "gauge", "", "Number of half-open IKE SAs."],
    ["ipsec_ike_sa_state", "gauge", "name, uid, remote_host", "Numeric IKE SA state (0=CREATED .. 7=DESTROYING)."],
    ["ipsec_ike_sa_established_seconds", "gauge", "name, uid, remote_host", "Seconds since the IKE SA was established."],
    ["ipsec_child_sa_state", "gauge", "ike_sa_name, name, uid, remote_host, local_ts, remote_ts", "Numeric child SA state (0=CREATED .. 10=DESTROYING)."],
    ["ipsec_child_sa_bytes_in", "gauge", "ike_sa_name, name, uid, remote_host, local_ts, remote_ts", "Bytes received on this child SA."],
    ["ipsec_child_sa_bytes_out", "gauge", "ike_sa_name, name, uid, remote_host, local_ts, remote_ts", "Bytes sent on this child SA."],
    ["ipsec_child_sa_packets_in", "gauge", "ike_sa_name, name, uid, remote_host, local_ts, remote_ts", "Packets received on this child SA."],
    ["ipsec_child_sa_packets_out", "gauge", "ike_sa_name, name, uid, remote_host, local_ts, remote_ts", "Packets sent on this child SA."],
    ["ipsec_child_sa_installed_seconds", "gauge", "ike_sa_name, name, uid, remote_host, local_ts, remote_ts", "Seconds since the child SA was installed."],
    ["ipsec_uptime_seconds", "gauge", "", "Charon daemon uptime in seconds."],
    ["ipsec_workers_total", "gauge", "", "Total number of charon worker threads."],
    ["ipsec_idle_workers", "gauge", "", "Number of idle charon worker threads."],
    ["ipsec_active_workers", "gauge", "", "Number of active charon worker threads."],
    ["ipsec_queues", "gauge", "priority", "Number of queued jobs by priority (critical, high, medium, low)."],
  ];

  const networkMetrics = [
    ["network_arp_entry", "gauge", "ip, mac, device, state", "ARP table entry; value is always 1."],
    ["network_interface_type", "gauge", "device, type, driver", "Interface type classification; value is always 1."],
    ["network_bond_member", "gauge", "bond, member", "Bond membership; value is always 1."],
    ["network_bridge_member", "gauge", "bridge, member", "Bridge membership; value is always 1."],
    ["network_graph_edge", "gauge", "remote_host, local_port, direction", "Network topology edge; value is always 1."],
    ["network_port_listen", "gauge", "port, protocol, bind_address", "Listening port presence; value is always 1."],
    ["network_port_connections", "gauge", "port, protocol, state", "Number of connections per port, protocol, and state."],
    ["network_port_bytes_in", "gauge", "port, protocol", "Total inbound bytes per port from conntrack."],
    ["network_port_bytes_out", "gauge", "port, protocol", "Total outbound bytes per port from conntrack."],
    ["network_conntrack_accounting_enabled", "gauge", "", "Whether conntrack accounting is available (1) or not (0)."],
    ["network_firewall_collector_up", "gauge", "", "Whether nftables counters could be read (1 = collecting, 0 = unavailable)."],
    ["network_firewall_drop_packets_total", "counter", "family, table, chain, rule", "Total packets dropped by nftables DROP rules."],
    ["network_firewall_drop_bytes_total", "counter", "family, table, chain, rule", "Total bytes dropped by nftables DROP rules."],
    ["network_firewall_reject_packets_total", "counter", "family, table, chain, rule", "Total packets rejected by nftables REJECT rules."],
    ["network_firewall_reject_bytes_total", "counter", "family, table, chain, rule", "Total bytes rejected by nftables REJECT rules."],
    ["network_firewall_policy_drop_packets_total", "counter", "family, table, chain", "Total packets dropped by chain default DROP policy."],
    ["network_firewall_policy_drop_bytes_total", "counter", "family, table, chain", "Total bytes dropped by chain default DROP policy."],
  ];

  const renderMetricTable = (metrics: string[][]): string => {
    if (metrics[0].length === 4) {
      const rows = metrics
        .map(([name, type, labels, desc]) => {
          const labelCell = labels ? `<code>${labels}</code>` : "";
          return `<tr><td><code>${name}</code></td><td>${type}</td><td>${labelCell}</td><td>${desc}</td></tr>`;
        })
        .join("");
      return `<table><thead><tr><th>Metric</th><th>Type</th><th>Labels</th><th>Description</th></tr></thead><tbody>${rows}</tbody></table>`;
    }
    const rows = metrics
      .map(([name, type, desc]) => `<tr><td><code>${name}</code></td><td>${type}</td><td>${desc}</td></tr>`)
      .join("");
    return `<table><thead><tr><th>Metric</th><th>Type</th><th>Description</th></tr></thead><tbody>${rows}</tbody></table>`;
  };

  const renderStateTable = (states: string[][]): string => {
    const rows = states
      .map(([val, name, desc]) => `<tr><td><code>${val}</code></td><td>${name}</td><td>${desc}</td></tr>`)
      .join("");
    return `<table><thead><tr><th>Value</th><th>State</th><th>Description</th></tr></thead><tbody>${rows}</tbody></table>`;
  };

  return `
    <div class="section">
      <h2>IPsec Exporter</h2>
      <p>
        <code>ipsec_exporter</code> is a Prometheus exporter for host network and IPsec
        tunnel metrics. It is a <strong>superset of <code>network_exporter</code></strong>:
        it includes all five network collectors (ARP, Interface, Network Graph, Conntrack,
        Firewall) and adds an IPsec collector that reports Security Association (SA) state
        and traffic counters obtained from the strongSwan VICI protocol.
      </p>
      <p>
        The exporter runs on hosts that terminate IPsec tunnels. It dials the strongSwan
        charon daemon's VICI Unix socket on every scrape to list IKE and child SAs and to
        retrieve daemon statistics. Tunnels are auto-discovered; no static configuration of
        tunnel names or peer addresses is required.
      </p>
    </div>

    <div class="section">
      <h2>Architecture</h2>
      <p>The exporter registers six collectors into a single Prometheus registry:</p>
      <table>
        <thead><tr><th>Collector</th><th>Source</th><th>Metrics Prefix</th></tr></thead>
        <tbody>
          <tr><td><strong>ARP</strong></td><td>netlink</td><td><code>network_arp_*</code></td></tr>
          <tr><td><strong>Interface</strong></td><td>sysfs</td><td><code>network_interface_*</code>, <code>network_bond_*</code>, <code>network_bridge_*</code></td></tr>
          <tr><td><strong>Network Graph</strong></td><td>procfs</td><td><code>network_graph_*</code></td></tr>
          <tr><td><strong>Conntrack</strong></td><td>procfs + netlink</td><td><code>network_port_*</code>, <code>network_conntrack_*</code></td></tr>
          <tr><td><strong>Firewall</strong></td><td>netlink nf_tables</td><td><code>network_firewall_*</code></td></tr>
          <tr><td><strong>IPsec</strong></td><td>strongSwan VICI socket</td><td><code>ipsec_*</code></td></tr>
        </tbody>
      </table>
      <p>
        The first five collectors are identical to those in <code>network_exporter</code>.
        The IPsec collector communicates with the strongSwan charon daemon over the VICI
        (Versatile IKE Configuration Interface) Unix socket, issuing <code>list-sas</code>
        and <code>stats</code> commands on each scrape.
      </p>
      <p>
        The HTTP handler is configured with <code>ContinueOnError</code> so that a failure
        in any single collector does not prevent the remaining collectors from returning their
        metrics. This is critical because a host is far more likely to be missing one data
        source than all six.
      </p>
    </div>

    <div class="section">
      <h2>Installation</h2>
      <p>Container images are published to GitHub Container Registry:</p>
      <pre><code>ghcr.io/phaseshiftdata/ipsec_exporter</code></pre>
      <pre><code># Run on an IPsec termination host
docker run -d \\
  --name ipsec_exporter \\
  --network host \\
  -v /var/run/charon.vici:/var/run/charon.vici:ro \\
  -v /proc:/host/proc:ro \\
  -v /sys:/host/sys:ro \\
  ghcr.io/phaseshiftdata/ipsec_exporter:main \\
  --proc-path /host/proc \\
  --sys-path /host/sys</code></pre>
      <p>
        The container requires access to the host's VICI socket, procfs, and sysfs.
        Running with <code>--network host</code> provides the netlink access needed by
        the ARP, Conntrack, and Firewall collectors.
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
      <h2>Metrics: IPsec</h2>
      <p>
        The IPsec collector queries the strongSwan VICI socket on every scrape, issuing
        <code>list-sas</code> to enumerate Security Associations and <code>stats</code>
        to retrieve charon daemon health data.
      </p>
      ${renderMetricTable(ipsecMetrics)}
    </div>

    <div class="section">
      <h2>IKE SA States</h2>
      <p>The <code>ipsec_ike_sa_state</code> metric reports a numeric state value:</p>
      ${renderStateTable(ikeStates)}
    </div>

    <div class="section">
      <h2>Child SA States</h2>
      <p>The <code>ipsec_child_sa_state</code> metric reports a numeric state value:</p>
      ${renderStateTable(childStates)}
    </div>

    <div class="section">
      <h2>Metrics: Network</h2>
      <p>
        The following network metrics are inherited from <code>network_exporter</code>.
        All five network collectors (ARP, Interface, Network Graph, Conntrack, Firewall)
        are included.
      </p>
      ${renderMetricTable(networkMetrics)}
    </div>

    <div class="section">
      <h2>Tunnel Auto-Discovery</h2>
      <p>
        Tunnels are discovered dynamically from the VICI response on every scrape. No static
        configuration of tunnel names or peer addresses is required.
      </p>
      <ul>
        <li>When a new tunnel is established between scrapes, it appears in the next scrape automatically.</li>
        <li>When a tunnel is removed or torn down, it disappears from the next scrape.</li>
        <li>During a rekey, both old and new SAs appear with distinct <code>uid</code> values. The old SA disappears once charon completes the transition.</li>
      </ul>
      <p>
        Use the <code>name</code> label to track a logical tunnel across rekeys, and the
        <code>uid</code> label to distinguish individual SA instances.
      </p>
    </div>

    <div class="section">
      <h2>VICI Protocol Details</h2>
      <p>
        The VICI (Versatile IKE Configuration Interface) protocol is strongSwan's programmatic
        control interface. It uses a Unix domain socket (default <code>/var/run/charon.vici</code>)
        and a binary message format. The exporter uses the
        <a href="https://github.com/strongswan/govici">govici</a> Go library.
      </p>
      <p>On each scrape the collector:</p>
      <ol>
        <li>Dials the VICI socket.</li>
        <li>Sends a <code>list-sas</code> streamed command, receiving one message per IKE SA with nested child SA data.</li>
        <li>Sends a <code>stats</code> command, receiving charon daemon health data.</li>
        <li>Closes the connection.</li>
      </ol>
      <p>
        The connection is not held open between scrapes. Each scrape creates a fresh VICI
        session, ensuring the exporter does not accumulate stale state or prevent charon
        from cleaning up resources.
      </p>
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
          <tr><td>VICI socket</td><td><code>/var/run/charon.vici</code></td><td>IPsec SA enumeration and charon statistics</td></tr>
          <tr><td>procfs</td><td><code>/proc</code></td><td>Network Graph and Conntrack collectors</td></tr>
          <tr><td>sysfs</td><td><code>/sys</code></td><td>Interface classification collector</td></tr>
          <tr><td>CAP_NET_ADMIN</td><td><em>capability</em></td><td>Firewall collector (nf_tables netlink)</td></tr>
        </tbody>
      </table>

      <h3>Kubernetes DaemonSet</h3>
      <pre><code>apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: ipsec-exporter
spec:
  selector:
    matchLabels:
      app: ipsec-exporter
  template:
    metadata:
      labels:
        app: ipsec-exporter
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9100"
    spec:
      hostNetwork: true
      hostPID: true
      nodeSelector:
        node-role.kubernetes.io/ipsec: ""
      containers:
        - name: ipsec-exporter
          image: ghcr.io/phaseshiftdata/ipsec_exporter:main
          args:
            - --listen-address=0.0.0.0:9100
            - --proc-path=/host/proc
            - --sys-path=/host/sys
            - --vici-socket=/host/run/charon.vici
          ports:
            - containerPort: 9100
              hostPort: 9100
          securityContext:
            capabilities:
              add:
                - NET_ADMIN
          volumeMounts:
            - name: proc
              mountPath: /host/proc
              readOnly: true
            - name: sys
              mountPath: /host/sys
              readOnly: true
            - name: vici
              mountPath: /host/run/charon.vici
              readOnly: true
      volumes:
        - name: proc
          hostPath:
            path: /proc
        - name: sys
          hostPath:
            path: /sys
        - name: vici
          hostPath:
            path: /var/run/charon.vici
            type: Socket</code></pre>

      <h3>Prometheus Scrape Config</h3>
      <pre><code>scrape_configs:
  - job_name: ipsec
    scrape_interval: 30s
    static_configs:
      - targets:
          - ipsec-host-1:9100
          - ipsec-host-2:9100</code></pre>
    </div>

    <div class="section">
      <h2>Failure Modes</h2>
      <table>
        <thead><tr><th>Scenario</th><th>Behavior</th></tr></thead>
        <tbody>
          <tr>
            <td><strong>VICI socket unavailable</strong></td>
            <td>The IPsec collector emits <code>ipsec_up 0</code> and no other <code>ipsec_*</code> metrics. All network collectors continue to function normally.</td>
          </tr>
          <tr>
            <td><strong>strongSwan not running</strong></td>
            <td>Same as VICI socket unavailable. The collector detects this via <code>IsAvailable()</code> which attempts to dial the socket.</td>
          </tr>
          <tr>
            <td><strong>Partial VICI failure</strong></td>
            <td>If <code>list-sas</code> succeeds but <code>stats</code> fails, IKE and child SA metrics are emitted normally. Charon health metrics (<code>ipsec_uptime_seconds</code>, <code>ipsec_workers_total</code>, etc.) are absent. <code>ipsec_up</code> remains 1.</td>
          </tr>
          <tr>
            <td><strong>ContinueOnError behavior</strong></td>
            <td>The <code>ContinueOnError</code> handler ensures a failure in any one collector does not prevent other collectors from returning metrics. A broken Firewall collector does not affect IPsec metrics, and vice versa.</td>
          </tr>
          <tr>
            <td><strong>Tunnel flap / rekey</strong></td>
            <td>Both old (REKEYED) and new (ESTABLISHED) SAs appear with distinct <code>uid</code> values during the transition. The old SA disappears once charon completes the handoff.</td>
          </tr>
        </tbody>
      </table>
    </div>`;
}
