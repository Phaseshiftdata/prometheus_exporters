(function(){let e=document.createElement(`link`).relList;if(e&&e.supports&&e.supports(`modulepreload`))return;for(let e of document.querySelectorAll(`link[rel="modulepreload"]`))n(e);new MutationObserver(e=>{for(let t of e)if(t.type===`childList`)for(let e of t.addedNodes)e.tagName===`LINK`&&e.rel===`modulepreload`&&n(e)}).observe(document,{childList:!0,subtree:!0});function t(e){let t={};return e.integrity&&(t.integrity=e.integrity),e.referrerPolicy&&(t.referrerPolicy=e.referrerPolicy),t.credentials=e.crossOrigin===`use-credentials`?`include`:e.crossOrigin===`anonymous`?`omit`:`same-origin`,t}function n(e){if(e.ep)return;e.ep=!0;let n=t(e);fetch(e.href,n)}})();function e(){return`
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
        <tbody>${[[`network_exporter`,`Network connectivity and performance metrics`,`9101`],[`ipsec_exporter`,`IPsec tunnel status and traffic metrics`,`9102`],[`cloudflare_exporter`,`Cloudflare analytics, Zero Trust, DNS, and certificate metrics`,`9199`],[`libvirt_exporter`,`Libvirt/KVM virtual machine and hypervisor metrics`,`9177`]].map(([e,t,n])=>`<tr><td><strong>${e}</strong></td><td>${t}</td><td>${n}</td></tr>`).join(``)}</tbody>
      </table>
    </div>
    <div class="section">
      <h2>Quick Start</h2>
      <pre><code># Pull an exporter image
docker pull ghcr.io/phaseshiftdata/cloudflare_exporter:main

# Run with an API token
docker run -d -p 9199:9199 \\
  -e CF_API_TOKEN="your-cloudflare-api-token" \\
  ghcr.io/phaseshiftdata/cloudflare_exporter:main</code></pre>
    </div>
    <div class="section">
      <h2>Container Images</h2>
      <p>Images are published to GitHub Container Registry (GHCR):</p>
      <pre><code>ghcr.io/phaseshiftdata/network_exporter
ghcr.io/phaseshiftdata/ipsec_exporter
ghcr.io/phaseshiftdata/cloudflare_exporter</code></pre>
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
git clone https://github.com/phaseshiftdata/prometheus_exporters.git
cd prometheus_exporters

# Build all exporter images
make build

# Run linters
make lint

# Run all tests
make test

# Push images to GHCR
make deploy</code></pre>
    </div>`}function t(){let e=[[`--listen-address`,`127.0.0.1:9100`,`Address and port the HTTP server listens on.`],[`--proc-path`,`/proc`,`Path to procfs mount. Set to <code>/host/proc</code> when running in a container with the host's procfs bind-mounted.`],[`--sys-path`,`/sys`,`Path to sysfs mount. Set to <code>/host/sys</code> when running in a container with the host's sysfs bind-mounted.`],[`--log-level`,`info`,`Log verbosity. One of <code>debug</code>, <code>info</code>, <code>warn</code>, <code>error</code>.`]].map(([e,t,n])=>`<tr><td><code>${e}</code></td><td><code>${t}</code></td><td>${n}</td></tr>`).join(``),t=[[`network_arp_entry`,`gauge`,`ip, mac, device, state`,`ARP table entry; value is always 1.`]],n=[[`network_interface_type`,`gauge`,`device, type, driver`,`Interface type classification; value is always 1.`],[`network_bond_member`,`gauge`,`bond, member`,`Bond membership; value is always 1.`],[`network_bridge_member`,`gauge`,`bridge, member`,`Bridge membership; value is always 1.`]],r=[[`network_graph_edge`,`gauge`,`remote_host, local_port, direction`,`Presence indicator for a network topology edge; value is always 1.`]],i=[[`network_port_connections`,`gauge`,`port, protocol, state`,`Number of connections per port, protocol, and state.`],[`network_port_listen`,`gauge`,`port, protocol, bind_address`,`Presence of a listening port; value is always 1.`],[`network_port_bytes_in`,`gauge`,`port, protocol`,`Total inbound bytes per port from conntrack.`],[`network_port_bytes_out`,`gauge`,`port, protocol`,`Total outbound bytes per port from conntrack.`],[`network_conntrack_accounting_enabled`,`gauge`,`<em>(none)</em>`,`Whether conntrack accounting is available (1) or not (0).`]],a=[[`network_firewall_collector_up`,`gauge`,`<em>(none)</em>`,`Whether nftables counters could be read (1 = collecting, 0 = unavailable).`],[`network_firewall_drop_packets_total`,`counter`,`family, table, chain, rule`,`Total packets dropped by nftables DROP rules.`],[`network_firewall_drop_bytes_total`,`counter`,`family, table, chain, rule`,`Total bytes dropped by nftables DROP rules.`],[`network_firewall_reject_packets_total`,`counter`,`family, table, chain, rule`,`Total packets rejected by nftables REJECT rules.`],[`network_firewall_reject_bytes_total`,`counter`,`family, table, chain, rule`,`Total bytes rejected by nftables REJECT rules.`],[`network_firewall_policy_drop_packets_total`,`counter`,`family, table, chain`,`Total packets dropped by chain default DROP policy.`],[`network_firewall_policy_drop_bytes_total`,`counter`,`family, table, chain`,`Total bytes dropped by chain default DROP policy.`]],o=e=>`<table><thead><tr><th>Metric</th><th>Type</th><th>Labels</th><th>Description</th></tr></thead><tbody>${e.map(([e,t,n,r])=>`<tr><td><code>${e}</code></td><td>${t}</td><td><code>${n}</code></td><td>${r}</td></tr>`).join(``)}</tbody></table>`;return`
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
        <tbody>${e}</tbody>
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
      ${o(t)}
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
      ${o(n)}
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
      ${o(r)}
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
      ${o(i)}
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
      ${o(a)}
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
    </div>`}function n(){let e=[[`--listen-address`,`127.0.0.1:9100`,`Address and port the HTTP server listens on.`],[`--proc-path`,`/proc`,`Path to the procfs mount point. Override when running in a container with procfs bind-mounted to a non-default path.`],[`--sys-path`,`/sys`,`Path to the sysfs mount point. Override when running in a container with sysfs bind-mounted to a non-default path.`],[`--vici-socket`,`/var/run/charon.vici`,`Path to the strongSwan VICI Unix socket.`],[`--log-level`,`info`,`Log verbosity. One of <code>debug</code>, <code>info</code>, <code>warn</code>, <code>error</code>.`]].map(([e,t,n])=>`<tr><td><code>${e}</code></td><td><code>${t}</code></td><td>${n}</td></tr>`).join(``),t=[[`0`,`CREATED`,`SA allocated but not yet negotiating.`],[`1`,`CONNECTING`,`IKE negotiation in progress.`],[`2`,`ESTABLISHED`,`SA is up and operational.`],[`3`,`PASSIVE`,`Passive SA (responder waiting).`],[`4`,`REKEYING`,`Rekey in progress.`],[`5`,`REKEYED`,`Old SA after a successful rekey (being replaced).`],[`6`,`DELETING`,`SA is being torn down.`],[`7`,`DESTROYING`,`SA is being destroyed.`]],n=[[`0`,`CREATED`,`Child SA allocated.`],[`1`,`ROUTED`,`Trap policy installed, waiting for traffic.`],[`2`,`INSTALLING`,`SPIs allocated, installing policies.`],[`3`,`INSTALLED`,`SA is installed and passing traffic.`],[`4`,`UPDATING`,`SA parameters are being updated.`],[`5`,`REKEYING`,`Rekey in progress.`],[`6`,`REKEYED`,`Old child SA after a successful rekey.`],[`7`,`RETRYING`,`Retrying negotiation after a failure.`],[`8`,`DELETING`,`Child SA is being deleted.`],[`9`,`DELETED`,`Child SA has been deleted.`],[`10`,`DESTROYING`,`Child SA is being destroyed.`]],r=[[`ipsec_up`,`gauge`,``,`Whether the VICI socket is reachable (1 = up, 0 = down).`],[`ipsec_ike_sas`,`gauge`,``,`Total number of IKE SAs.`],[`ipsec_half_open_ike_sas`,`gauge`,``,`Number of half-open IKE SAs.`],[`ipsec_ike_sa_state`,`gauge`,`name, uid, remote_host`,`Numeric IKE SA state (0=CREATED .. 7=DESTROYING).`],[`ipsec_ike_sa_established_seconds`,`gauge`,`name, uid, remote_host`,`Seconds since the IKE SA was established.`],[`ipsec_child_sa_state`,`gauge`,`ike_sa_name, name, uid, remote_host, local_ts, remote_ts`,`Numeric child SA state (0=CREATED .. 10=DESTROYING).`],[`ipsec_child_sa_bytes_in`,`gauge`,`ike_sa_name, name, uid, remote_host, local_ts, remote_ts`,`Bytes received on this child SA.`],[`ipsec_child_sa_bytes_out`,`gauge`,`ike_sa_name, name, uid, remote_host, local_ts, remote_ts`,`Bytes sent on this child SA.`],[`ipsec_child_sa_packets_in`,`gauge`,`ike_sa_name, name, uid, remote_host, local_ts, remote_ts`,`Packets received on this child SA.`],[`ipsec_child_sa_packets_out`,`gauge`,`ike_sa_name, name, uid, remote_host, local_ts, remote_ts`,`Packets sent on this child SA.`],[`ipsec_child_sa_installed_seconds`,`gauge`,`ike_sa_name, name, uid, remote_host, local_ts, remote_ts`,`Seconds since the child SA was installed.`],[`ipsec_uptime_seconds`,`gauge`,``,`Charon daemon uptime in seconds.`],[`ipsec_workers_total`,`gauge`,``,`Total number of charon worker threads.`],[`ipsec_idle_workers`,`gauge`,``,`Number of idle charon worker threads.`],[`ipsec_active_workers`,`gauge`,``,`Number of active charon worker threads.`],[`ipsec_queues`,`gauge`,`priority`,`Number of queued jobs by priority (critical, high, medium, low).`]],i=[[`network_arp_entry`,`gauge`,`ip, mac, device, state`,`ARP table entry; value is always 1.`],[`network_interface_type`,`gauge`,`device, type, driver`,`Interface type classification; value is always 1.`],[`network_bond_member`,`gauge`,`bond, member`,`Bond membership; value is always 1.`],[`network_bridge_member`,`gauge`,`bridge, member`,`Bridge membership; value is always 1.`],[`network_graph_edge`,`gauge`,`remote_host, local_port, direction`,`Network topology edge; value is always 1.`],[`network_port_listen`,`gauge`,`port, protocol, bind_address`,`Listening port presence; value is always 1.`],[`network_port_connections`,`gauge`,`port, protocol, state`,`Number of connections per port, protocol, and state.`],[`network_port_bytes_in`,`gauge`,`port, protocol`,`Total inbound bytes per port from conntrack.`],[`network_port_bytes_out`,`gauge`,`port, protocol`,`Total outbound bytes per port from conntrack.`],[`network_conntrack_accounting_enabled`,`gauge`,``,`Whether conntrack accounting is available (1) or not (0).`],[`network_firewall_collector_up`,`gauge`,``,`Whether nftables counters could be read (1 = collecting, 0 = unavailable).`],[`network_firewall_drop_packets_total`,`counter`,`family, table, chain, rule`,`Total packets dropped by nftables DROP rules.`],[`network_firewall_drop_bytes_total`,`counter`,`family, table, chain, rule`,`Total bytes dropped by nftables DROP rules.`],[`network_firewall_reject_packets_total`,`counter`,`family, table, chain, rule`,`Total packets rejected by nftables REJECT rules.`],[`network_firewall_reject_bytes_total`,`counter`,`family, table, chain, rule`,`Total bytes rejected by nftables REJECT rules.`],[`network_firewall_policy_drop_packets_total`,`counter`,`family, table, chain`,`Total packets dropped by chain default DROP policy.`],[`network_firewall_policy_drop_bytes_total`,`counter`,`family, table, chain`,`Total bytes dropped by chain default DROP policy.`]],a=e=>e[0].length===4?`<table><thead><tr><th>Metric</th><th>Type</th><th>Labels</th><th>Description</th></tr></thead><tbody>${e.map(([e,t,n,r])=>`<tr><td><code>${e}</code></td><td>${t}</td><td>${n?`<code>${n}</code>`:``}</td><td>${r}</td></tr>`).join(``)}</tbody></table>`:`<table><thead><tr><th>Metric</th><th>Type</th><th>Description</th></tr></thead><tbody>${e.map(([e,t,n])=>`<tr><td><code>${e}</code></td><td>${t}</td><td>${n}</td></tr>`).join(``)}</tbody></table>`,o=e=>`<table><thead><tr><th>Value</th><th>State</th><th>Description</th></tr></thead><tbody>${e.map(([e,t,n])=>`<tr><td><code>${e}</code></td><td>${t}</td><td>${n}</td></tr>`).join(``)}</tbody></table>`;return`
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
docker run -d --rm \\
  --name ipsec_exporter \\
  --network=host --pid=host \\
  --user 0:0 \\
  --cap-add=NET_ADMIN \\
  --cap-add=DAC_READ_SEARCH \\
  --security-opt label=disable \\
  -v /proc:/host/proc:ro \\
  -v /sys:/host/sys:ro \\
  -v /var/run/charon.vici:/var/run/charon.vici:ro \\
  ghcr.io/phaseshiftdata/ipsec_exporter:main \\
  --proc-path=/host/proc \\
  --sys-path=/host/sys \\
  --vici-socket=/var/run/charon.vici \\
  --listen-address=0.0.0.0:9100</code></pre>
      <p>
        The container requires access to the host's VICI socket, procfs, and sysfs.
        It must run as root (<code>--user 0:0</code>) because reading procfs entries
        of other processes and querying nftables counters require root privileges.
        The <code>--network=host</code> and <code>--pid=host</code> flags provide
        access to the host's network and PID namespaces. On SELinux hosts,
        <code>--security-opt label=disable</code> is required.
      </p>
    </div>

    <div class="section">
      <h2>Configuration</h2>
      <p>All configuration is via CLI flags. No configuration file or environment variables are required.</p>
      <table>
        <thead><tr><th>Flag</th><th>Default</th><th>Description</th></tr></thead>
        <tbody>${e}</tbody>
      </table>
    </div>

    <div class="section">
      <h2>Metrics: IPsec</h2>
      <p>
        The IPsec collector queries the strongSwan VICI socket on every scrape, issuing
        <code>list-sas</code> to enumerate Security Associations and <code>stats</code>
        to retrieve charon daemon health data.
      </p>
      ${a(r)}
    </div>

    <div class="section">
      <h2>IKE SA States</h2>
      <p>The <code>ipsec_ike_sa_state</code> metric reports a numeric state value:</p>
      ${o(t)}
    </div>

    <div class="section">
      <h2>Child SA States</h2>
      <p>The <code>ipsec_child_sa_state</code> metric reports a numeric state value:</p>
      ${o(n)}
    </div>

    <div class="section">
      <h2>Metrics: Network</h2>
      <p>
        The following network metrics are inherited from <code>network_exporter</code>.
        All five network collectors (ARP, Interface, Network Graph, Conntrack, Firewall)
        are included.
      </p>
      ${a(i)}
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
      <p>
        The exporter must run as root (<code>--user 0:0</code>) because reading
        procfs entries of other processes and querying nftables counters require
        root privileges. The default distroless UID 65532 is not sufficient.
      </p>
      <table>
        <thead><tr><th>Resource</th><th>Mount / Flag</th><th>Purpose</th></tr></thead>
        <tbody>
          <tr><td>VICI socket</td><td><code>-v /var/run/charon.vici:/var/run/charon.vici:ro</code></td><td>IPsec SA enumeration and charon statistics</td></tr>
          <tr><td>procfs</td><td><code>-v /proc:/host/proc:ro</code> + <code>--proc-path=/host/proc</code></td><td>Network Graph and Conntrack collectors</td></tr>
          <tr><td>sysfs</td><td><code>-v /sys:/host/sys:ro</code> + <code>--sys-path=/host/sys</code></td><td>Interface classification collector</td></tr>
          <tr><td>Host PID namespace</td><td><code>--pid=host</code></td><td>Required to see the host's <code>/proc/net/</code></td></tr>
          <tr><td>Host network namespace</td><td><code>--network=host</code></td><td>Required for netlink sockets in host network namespace</td></tr>
          <tr><td>Root user</td><td><code>--user 0:0</code></td><td>Required for procfs access and nftables counters</td></tr>
          <tr><td>CAP_NET_ADMIN</td><td><code>--cap-add=NET_ADMIN</code></td><td>Firewall collector (nf_tables netlink), conntrack, neighbor table</td></tr>
          <tr><td>CAP_DAC_READ_SEARCH</td><td><code>--cap-add=DAC_READ_SEARCH</code></td><td>Reading <code>/proc</code> entries of other processes</td></tr>
          <tr><td>SELinux label disable</td><td><code>--security-opt label=disable</code></td><td>Required on SELinux hosts for procfs/sysfs access</td></tr>
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
    </div>`}function r(){let e=[[`CF_API_TOKEN`,`<em>(required)</em>`,`Cloudflare API token with appropriate permissions.`],[`CF_ACCOUNTS`,`<em>(auto-discovered)</em>`,`Comma-separated account IDs to monitor. When omitted, all accounts visible to the token are used.`],[`CF_ZONES`,`<em>(auto-discovered)</em>`,`Comma-separated zone IDs to monitor. When omitted, all zones visible to the token are used.`],[`CF_ZONES_EXCLUDE`,`<em>(empty)</em>`,`Comma-separated zone IDs to exclude from monitoring.`],[`CF_SCRAPE_DELAY_SECONDS`,`300`,`Propagation delay (seconds) subtracted from "now" when querying analytics. Cloudflare's pipeline has roughly a five-minute lag.`],[`CF_TIME_WINDOW_SECONDS`,`60`,`Width of each query window (seconds) for each collection cycle.`],[`CF_REFRESH_INTERVAL_SECONDS`,`60`,`How often (seconds) the scheduler triggers a new collection cycle.`],[`CF_DISCOVERY_INTERVAL_SECONDS`,`21600`,`How often (seconds) capability discovery re-runs to pick up new zones or product entitlements. Default is six hours.`],[`CF_GRAPHQL_BUDGET_PER_WINDOW`,`160`,`Maximum GraphQL API calls allowed per five-minute sliding window.`],[`CF_REST_BUDGET_PER_WINDOW`,`600`,`Maximum REST API calls allowed per five-minute sliding window.`],[`CF_COLLECTORS_ENABLED`,`<em>(all discovered)</em>`,`Comma-separated list of collector names to enable. When omitted, all collectors whose capabilities are satisfied are enabled.`],[`CF_GATEWAY_CATEGORY_ALLOWLIST`,`<em>(empty)</em>`,`Comma-separated allowlist of Gateway content categories to track.`],[`CF_GATEWAY_CATEGORY_TOP_N`,`25`,`Maximum number of top Gateway categories to retain per cycle (limits label cardinality).`],[`CF_REQUEST_TIMEOUT_SECONDS`,`10`,`Per-request timeout (seconds) for Cloudflare API calls.`],[`LISTEN_ADDRESS`,`:9199`,`Address and port the HTTP server listens on.`],[`METRICS_PATH`,`/metrics`,`HTTP path where Prometheus metrics are served.`],[`LOG_LEVEL`,`info`,`Log verbosity. One of debug, info, warn, error.`]].map(([e,t,n])=>`<tr><td><code>${e}</code></td><td><code>${t}</code></td><td>${n}</td></tr>`).join(``),t=[[`access_login_requests_total`,`counter`,`account, app_name, action`,`Total Access login requests.`],[`gateway_dns_queries_total`,`counter`,`account, location, policy, category, action`,`Total Gateway DNS queries.`],[`gateway_network_sessions_total`,`counter`,`account, policy, action`,`Total Gateway network sessions.`],[`gateway_network_bytes_total`,`counter`,`account, policy, direction`,`Total bytes through Gateway network sessions.`],[`browser_isolation_sessions_total`,`counter`,`account`,`Total Browser Isolation sessions.`],[`tunnel_requests_total`,`counter`,`account, tunnel_name`,`Total requests proxied through a Cloudflare Tunnel.`],[`tunnel_info`,`gauge`,`account, tunnel_name, tunnel_id, status`,`Tunnel metadata; value is always 1.`]],n=[[`dns_queries_total`,`counter`,`zone, query_type, response_code`,`Total authoritative DNS queries for a zone.`],[`dns_query_duration_seconds`,`histogram`,`zone`,`DNS query processing time distribution.`],[`dns_firewall_queries_total`,`counter`,`zone, cluster, response_code`,`Total queries handled by DNS Firewall.`]],r=[[`domain_expiration_timestamp_seconds`,`gauge`,`zone, domain`,`Unix timestamp when the domain registration expires.`],[`domain_auto_renew`,`gauge`,`zone, domain`,`1 if auto-renew is enabled, 0 otherwise.`],[`domain_locked`,`gauge`,`zone, domain`,`1 if the domain is locked, 0 otherwise.`],[`zone_status`,`gauge`,`zone, status`,`1 for the current zone status (active, pending, etc.).`],[`certificate_expiration_timestamp_seconds`,`gauge`,`zone, hostname, issuer`,`Unix timestamp when the SSL certificate expires.`]],i=[[`cloudflare_exporter_scrape_duration_seconds`,`histogram`,`Time spent collecting metrics from Cloudflare per cycle.`],[`cloudflare_exporter_scrape_errors_total`,`counter`,`Total collection errors by collector name.`],[`cloudflare_exporter_graphql_requests_total`,`counter`,`Total GraphQL API requests made.`],[`cloudflare_exporter_rest_requests_total`,`counter`,`Total REST API requests made.`],[`cloudflare_exporter_graphql_budget_remaining`,`gauge`,`Remaining GraphQL calls in the current budget window.`],[`cloudflare_exporter_rest_budget_remaining`,`gauge`,`Remaining REST calls in the current budget window.`],[`cloudflare_exporter_discovery_runs_total`,`counter`,`Total capability discovery runs.`],[`cloudflare_exporter_active_collectors`,`gauge`,`Number of currently active collectors.`],[`cloudflare_exporter_build_info`,`gauge`,`Build metadata (version, commit, Go version). Value is always 1.`]],a=e=>e[0].length===4?`<table><thead><tr><th>Metric</th><th>Type</th><th>Labels</th><th>Description</th></tr></thead><tbody>${e.map(([e,t,n,r])=>`<tr><td><code>${e}</code></td><td>${t}</td><td><code>${n}</code></td><td>${r}</td></tr>`).join(``)}</tbody></table>`:`<table><thead><tr><th>Metric</th><th>Type</th><th>Description</th></tr></thead><tbody>${e.map(([e,t,n])=>`<tr><td><code>${e}</code></td><td>${t}</td><td>${n}</td></tr>`).join(``)}</tbody></table>`;return`
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
        <tbody>${e}</tbody>
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
      ${a(t)}
    </div>

    <div class="section">
      <h2>Metrics: DNS</h2>
      ${a(n)}
    </div>

    <div class="section">
      <h2>Metrics: Domain &amp; Certificate Lifecycle</h2>
      ${a(r)}
    </div>

    <div class="section">
      <h2>Metrics: Self-Instrumentation</h2>
      ${a(i)}
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
    </div>`}function i(){let e=[[`--listen-address`,`127.0.0.1:9102`,`Address to listen on for metrics.`],[`--database-url`,`<em>(empty)</em>`,`PostgreSQL connection string (or <code>DATABASE_URL</code> env).`],[`--database-password-file`,`<em>(empty)</em>`,`Path to file containing the database password (substituted into <code>--database-url</code>).`],[`--database-password-openbao`,`<em>(empty)</em>`,`OpenBao KV path:field for the database password.`],[`--github-app-id`,`0`,`GitHub App ID.`],[`--github-install-id`,`0`,`GitHub App Installation ID.`],[`--github-key-file`,`<em>(empty)</em>`,`Path to GitHub App private key PEM file.`],[`--poll-interval`,`5m`,`Polling interval for fresh data.`],[`--org`,`<em>(empty)</em>`,`GitHub organization to monitor.`],[`--log-level`,`info`,`Log level (debug, info, warn, error).`],[`--job-budget-per-poll`,`50`,`Maximum workflow-job requests one poll cycle may issue across all repos.`],[`--backfill-interval`,`2s`,`Minimum spacing between historical backfill requests.`],[`--backfill-min-rate-limit`,`500`,`Pause backfill when remaining GitHub rate limit is below this.`],[`--openbao-address`,`<em>(empty)</em>`,`OpenBao/Vault server address (env: <code>OPENBAO_ADDR</code>).`],[`--openbao-approle-role-id-file`,`<em>(empty)</em>`,`Path to file containing the AppRole role_id.`],[`--openbao-approle-secret-id-file`,`<em>(empty)</em>`,`Path to file containing the AppRole secret_id.`]].map(([e,t,n])=>`<tr><td><code>${e}</code></td><td><code>${t}</code></td><td>${n}</td></tr>`).join(``),t=[[`github_exporter_workflow_runs_total`,`counter`,`repo, workflow, conclusion`,`Total workflow runs observed, by repo, workflow, and conclusion.`],[`github_exporter_open_pull_requests`,`gauge`,`repo`,`Current number of open pull requests per repo.`],[`github_exporter_commits_total`,`counter`,`repo`,`Total commits observed per repo.`]],n=[[`github_exporter_rate_limit_remaining`,`gauge`,``,`GitHub API primary rate limit remaining.`],[`github_exporter_rate_limited_total`,`counter`,`kind`,`403 responses from GitHub, by which rate limit produced them (primary, secondary, none).`]],r=[[`github_exporter_poll_duration_seconds`,`histogram`,``,`Duration of a complete poll cycle in seconds.`],[`github_exporter_scrape_errors_total`,`counter`,`collector`,`Total scrape errors by collector (repos, workflows, pullrequests, commits, tags, backfill).`],[`github_exporter_last_success_timestamp_seconds`,`gauge`,`collector`,`Unix timestamp of last successful scrape by collector.`]],i=[[`github_exporter_api_requests_total`,`counter`,`activity, kind`,`GitHub API requests issued, by activity (poll, backfill) and kind (runs_page, jobs).`],[`github_exporter_job_fetches_skipped_total`,`counter`,``,`Workflow runs whose jobs were already stored and unchanged, so no request was made.`],[`github_exporter_job_budget_exhausted_total`,`counter`,``,`Poll cycles that hit their job-request budget and deferred the rest to the backfiller.`]],a=[[`github_exporter_backfill_pending_job_runs`,`gauge`,``,`Stored workflow runs still waiting for their jobs to be fetched.`],[`github_exporter_backfill_repos_complete`,`gauge`,``,`Repositories whose historical run pagination has reached the end.`],[`github_exporter_backfill_repos_total`,`gauge`,``,`Repositories in the backfill rotation.`],[`github_exporter_backfill_throttled_total`,`counter`,`reason`,`Backfill ticks that deliberately issued no request, by reason.`],[`github_exporter_backfill_paused`,`gauge`,``,`1 while the backfiller is holding back to protect the rate limit, 0 otherwise.`],[`github_exporter_backfill_last_step_timestamp_seconds`,`gauge`,``,`Unix timestamp of the last backfill tick, whether or not it issued a request.`]],o=e=>`<table><thead><tr><th>Metric</th><th>Type</th><th>Labels</th><th>Description</th></tr></thead><tbody>${e.map(([e,t,n,r])=>`<tr><td><code>${e}</code></td><td>${t}</td><td>${n?`<code>${n}</code>`:`<em>none</em>`}</td><td>${r}</td></tr>`).join(``)}</tbody></table>`;return`
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
        <tbody>${e}</tbody>
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
      ${o(t)}
    </div>

    <div class="section">
      <h2>Metrics: Rate Limit</h2>
      ${o(n)}
    </div>

    <div class="section">
      <h2>Metrics: Poll Cycle</h2>
      ${o(r)}
    </div>

    <div class="section">
      <h2>Metrics: Request Accounting</h2>
      ${o(i)}
    </div>

    <div class="section">
      <h2>Metrics: Backfill</h2>
      ${o(a)}
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
    </div>`}function a(){let e=[[`--listen-address`,`127.0.0.1:9177`,`Address and port the HTTP server listens on.`],[`--libvirt-uri`,`qemu:///system`,`Libvirt connection URI. Use <code>qemu:///system</code> for the system daemon or <code>qemu+tcp://host/system</code> for remote TCP connections.`],[`--log-level`,`info`,`Log verbosity. One of <code>debug</code>, <code>info</code>, <code>warn</code>, <code>error</code>.`]].map(([e,t,n])=>`<tr><td><code>${e}</code></td><td><code>${t}</code></td><td>${n}</td></tr>`).join(``),t=[[`1`,`RUNNING`,`The domain is actively running.`],[`2`,`BLOCKED`,`The domain is blocked on a resource (waiting for I/O).`],[`3`,`PAUSED`,`The domain has been paused by the user.`],[`4`,`SHUTDOWN`,`The domain is in the process of shutting down.`],[`5`,`SHUTOFF`,`The domain is shut off (not running).`],[`6`,`CRASHED`,`The domain has crashed.`],[`7`,`SUSPENDED`,`The domain is suspended (saved to disk).`]],n=[[`libvirt_up`,`gauge`,``,`Whether the libvirt daemon is reachable (1 = up, 0 = down).`],[`libvirt_domains_total`,`gauge`,``,`Total number of defined domains (VMs).`],[`libvirt_host_cpu_count`,`gauge`,``,`Number of physical CPUs on the hypervisor host.`],[`libvirt_host_memory_bytes`,`gauge`,``,`Total physical memory on the hypervisor host in bytes.`],[`libvirt_host_free_memory_bytes`,`gauge`,``,`Free physical memory on the hypervisor host in bytes.`]],r=[[`libvirt_domain_info_state`,`gauge`,`domain, uuid`,`Numeric domain state (see Domain States table).`],[`libvirt_domain_info_max_memory_bytes`,`gauge`,`domain, uuid`,`Maximum allowed memory for the domain in bytes.`],[`libvirt_domain_info_memory_bytes`,`gauge`,`domain, uuid`,`Current memory allocation for the domain in bytes.`],[`libvirt_domain_info_vcpus`,`gauge`,`domain, uuid`,`Number of virtual CPUs assigned to the domain.`],[`libvirt_domain_cpu_time_seconds_total`,`counter`,`domain, uuid`,`Total CPU time consumed by the domain in seconds.`]],i=[[`libvirt_domain_memory_stats_bytes`,`gauge`,`domain, uuid, stat`,`Memory statistics for the domain. The <code>stat</code> label identifies the statistic (e.g. <code>actual</code>, <code>rss</code>, <code>unused</code>, <code>available</code>, <code>usable</code>, <code>swap_in</code>, <code>swap_out</code>).`]],a=[[`libvirt_domain_block_read_bytes_total`,`counter`,`domain, uuid, device`,`Total bytes read from the block device.`],[`libvirt_domain_block_read_requests_total`,`counter`,`domain, uuid, device`,`Total read requests issued to the block device.`],[`libvirt_domain_block_write_bytes_total`,`counter`,`domain, uuid, device`,`Total bytes written to the block device.`],[`libvirt_domain_block_write_requests_total`,`counter`,`domain, uuid, device`,`Total write requests issued to the block device.`]],o=[[`libvirt_domain_net_receive_bytes_total`,`counter`,`domain, uuid, interface`,`Total bytes received on the network interface.`],[`libvirt_domain_net_receive_packets_total`,`counter`,`domain, uuid, interface`,`Total packets received on the network interface.`],[`libvirt_domain_net_receive_errors_total`,`counter`,`domain, uuid, interface`,`Total receive errors on the network interface.`],[`libvirt_domain_net_transmit_bytes_total`,`counter`,`domain, uuid, interface`,`Total bytes transmitted on the network interface.`],[`libvirt_domain_net_transmit_packets_total`,`counter`,`domain, uuid, interface`,`Total packets transmitted on the network interface.`],[`libvirt_domain_net_transmit_errors_total`,`counter`,`domain, uuid, interface`,`Total transmit errors on the network interface.`]],s=e=>`<table><thead><tr><th>Metric</th><th>Type</th><th>Labels</th><th>Description</th></tr></thead><tbody>${e.map(([e,t,n,r])=>`<tr><td><code>${e}</code></td><td>${t}</td><td>${n?`<code>${n}</code>`:``}</td><td>${r}</td></tr>`).join(``)}</tbody></table>`;return`
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
        <tbody>${e}</tbody>
      </table>
    </div>

    <div class="section">
      <h2>Metrics: Hypervisor</h2>
      <p>
        Hypervisor-level metrics report the overall state of the libvirt host, including
        connectivity, domain count, CPU count, and memory.
      </p>
      ${s(n)}
    </div>

    <div class="section">
      <h2>Metrics: Domain Info</h2>
      <p>
        Per-domain metrics carry the labels <code>domain</code> (VM name) and <code>uuid</code>
        (libvirt UUID). Domain info metrics report the state, memory allocation, vCPU count,
        and cumulative CPU time for each domain.
      </p>
      ${s(r)}
    </div>

    <div class="section">
      <h2>Metrics: Memory Statistics</h2>
      <p>
        Memory statistics are reported per domain with a <code>stat</code> label identifying
        the specific statistic. The available statistics depend on the guest agent and
        hypervisor capabilities.
      </p>
      ${s(i)}
    </div>

    <div class="section">
      <h2>Metrics: Block Devices</h2>
      <p>
        Block device metrics report read and write byte and request counters per device.
        The <code>device</code> label identifies the block device (e.g. <code>vda</code>,
        <code>sda</code>).
      </p>
      ${s(a)}
    </div>

    <div class="section">
      <h2>Metrics: Network Interfaces</h2>
      <p>
        Network interface metrics report receive and transmit byte, packet, and error counters.
        The <code>interface</code> label identifies the virtual network interface (e.g.
        <code>vnet0</code>, <code>vnet1</code>).
      </p>
      ${s(o)}
    </div>

    <div class="section">
      <h2>Domain States</h2>
      <p>The <code>libvirt_domain_info_state</code> metric reports a numeric state value corresponding to the libvirt domain lifecycle:</p>
      ${(e=>`<table><thead><tr><th>Value</th><th>State</th><th>Description</th></tr></thead><tbody>${e.map(([e,t,n])=>`<tr><td><code>${e}</code></td><td>${t}</td><td>${n}</td></tr>`).join(``)}</tbody></table>`)(t)}
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
    </div>`}var o=`0.1.0`,s=Object.create(null);s[`/`]=e,s[`/network-exporter`]=t,s[`/ipsec-exporter`]=n,s[`/cloudflare-exporter`]=r,s[`/github-exporter`]=i,s[`/libvirt-exporter`]=a;function c(){let e=window.location.hash.replace(/^#\/?/,`/`);return e===``?`/`:e}function l(e){return`<nav class="nav">
    <a href="#/" class="nav-brand">prometheus_exporters</a>
    <div class="nav-links">${[{to:`/`,label:`Home`,exact:!0},{to:`/network-exporter`,label:`Network`},{to:`/ipsec-exporter`,label:`IPsec`},{to:`/cloudflare-exporter`,label:`Cloudflare`},{to:`/github-exporter`,label:`GitHub`},{to:`/libvirt-exporter`,label:`Libvirt`}].map(t=>{let n=t.exact?e===t.to:e.startsWith(t.to);return`<a href="#${t.to}" class="${n?`active`:``}">${t.label}</a>`}).join(``)}</div>
  </nav>`}function u(){return`<footer class="footer" role="contentinfo">
    <div class="footer-inner">
      <span>v${o}</span>
      <span>MIT License \u00A9 2026 Asymmetric Effort, LLC</span>
      <span>
        <a href="https://github.com/phaseshiftdata/prometheus_exporters" target="_blank" rel="noopener noreferrer">GitHub</a>
        \u00B7
        <a href="https://github.com/phaseshiftdata/prometheus_exporters/blob/main/SECURITY.md" target="_blank" rel="noopener noreferrer">Security</a>
        \u00B7
        <a href="https://github.com/phaseshiftdata/prometheus_exporters/blob/main/CONTRIBUTING.md" target="_blank" rel="noopener noreferrer">Contributing</a>
      </span>
    </div>
  </footer>`}function d(){let e=c(),t=document.getElementById(`root`),n=e in s?s[e]:s[`/`];t.innerHTML=`
    ${l(e)}
    <main class="main">${n()}</main>
    ${u()}
  `,f(e)}function f(e){let t=Object.create(null);t[`/`]=`Prometheus Exporters — Network, IPsec & Cloudflare`,t[`/network-exporter`]=`Network Exporter — Prometheus Exporters`,t[`/ipsec-exporter`]=`IPsec Exporter — Prometheus Exporters`,t[`/cloudflare-exporter`]=`Cloudflare Exporter — Prometheus Exporters`,t[`/github-exporter`]=`GitHub Exporter — Prometheus Exporters`,t[`/libvirt-exporter`]=`Libvirt Exporter — Prometheus Exporters`,document.title=e in t?t[e]:t[`/`];let n=document.querySelector(`link[rel="canonical"]`);n||(n=document.createElement(`link`),n.rel=`canonical`,document.head.appendChild(n)),n.href=`https://prometheus_exporters.phaseshiftdata.com/${e===`/`?``:`#`+e}`}d(),window.addEventListener(`hashchange`,d);