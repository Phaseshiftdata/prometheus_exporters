# IPsec Exporter

## Overview

`ipsec_exporter` is a Prometheus exporter for host network and IPsec tunnel
metrics. It is a **superset of `network_exporter`**: it includes all five
network collectors (ARP, Interface, Network Graph, Conntrack, Firewall) and
adds an IPsec collector that reports Security Association (SA) state and
traffic counters obtained from the strongSwan VICI protocol.

The exporter runs on hosts that terminate IPsec tunnels. It dials the
strongSwan charon daemon's VICI Unix socket on every scrape to list IKE and
child SAs and to retrieve daemon statistics. Tunnels are auto-discovered;
no static configuration of tunnel names or peer addresses is required.

## Architecture

The exporter registers six collectors into a single Prometheus registry:

| Collector | Source | Metrics Prefix |
| --- | --- | --- |
| ARP | netlink | `network_arp_*` |
| Interface | sysfs | `network_interface_*`, `network_bond_*`, `network_bridge_*` |
| Network Graph | procfs (`/proc/net/tcp`, `/proc/net/udp`) | `network_graph_*` |
| Conntrack | procfs + netlink conntrack | `network_port_*`, `network_conntrack_*` |
| Firewall | netlink nf_tables | `network_firewall_*` |
| IPsec | strongSwan VICI socket | `ipsec_*` |

The first five collectors are identical to those in `network_exporter`.
The IPsec collector communicates with the strongSwan charon daemon over the
VICI (Versatile IKE Configuration Interface) Unix socket, issuing
`list-sas` and `stats` commands on each scrape.

The HTTP handler is configured with `ContinueOnError` so that a failure in
any single collector (for example, the VICI socket being unavailable) does
not prevent the remaining collectors from returning their metrics. This is
critical because the host is far more likely to be missing one data source
than all six.

## Installation

Container images are published to GitHub Container Registry:

```
ghcr.io/phaseshiftdata/ipsec_exporter
```

### Docker

```bash
docker run -d --rm \
  --name ipsec_exporter \
  --network=host --pid=host \
  --user 0:0 \
  --cap-add=NET_ADMIN \
  --cap-add=DAC_READ_SEARCH \
  --security-opt label=disable \
  -v /proc:/host/proc:ro \
  -v /sys:/host/sys:ro \
  -v /var/run/charon.vici:/var/run/charon.vici:ro \
  ghcr.io/phaseshiftdata/ipsec_exporter:main \
  --proc-path=/host/proc \
  --sys-path=/host/sys \
  --vici-socket=/var/run/charon.vici \
  --listen-address=0.0.0.0:9100
```

### Host Requirements

The container requires access to several host resources. It must run as
root (`--user 0:0`) because reading procfs entries of other processes and
querying nftables counters require root privileges. The default
distroless UID 65532 is not sufficient. The container image does not use
file capabilities (`setcap`), so it is compatible with hardened Docker
environments that enable `userns-remap` and `no-new-privileges`.
Capabilities are provided at runtime via `cap_add` and take effect
because the process runs as root.

| Resource | Mount / Flag | Purpose |
| --- | --- | --- |
| VICI socket | `-v /var/run/charon.vici:/var/run/charon.vici:ro` | IPsec SA enumeration and charon statistics |
| procfs | `-v /proc:/host/proc:ro` + `--proc-path=/host/proc` | Network Graph and Conntrack collectors |
| sysfs | `-v /sys:/host/sys:ro` + `--sys-path=/host/sys` | Interface classification collector |
| Host PID namespace | `--pid=host` | Required to see the host's `/proc/net/` rather than the container's |
| Host network namespace | `--network=host` | Required so netlink sockets operate in the host's network namespace |
| Root user | `--user 0:0` | Required for reading procfs entries of other processes and nftables counters |
| `CAP_NET_ADMIN` | `--cap-add=NET_ADMIN` | Firewall collector (nf_tables netlink), conntrack flow queries, neighbor table access |
| `CAP_DAC_READ_SEARCH` | `--cap-add=DAC_READ_SEARCH` | Reading `/proc` entries of other processes |
| SELinux label disable | `--security-opt label=disable` | Required on SELinux hosts to read host procfs and sysfs mounts |

## Configuration

All configuration is via CLI flags. No configuration file or environment
variables are required.

| Flag | Default | Description |
| --- | --- | --- |
| `--listen-address` | `127.0.0.1:9100` | Address and port the HTTP server listens on. |
| `--proc-path` | `/proc` | Path to the procfs mount point. Override when running in a container with procfs bind-mounted to a non-default path. |
| `--sys-path` | `/sys` | Path to the sysfs mount point. Override when running in a container with sysfs bind-mounted to a non-default path. |
| `--vici-socket` | `/var/run/charon.vici` | Path to the strongSwan VICI Unix socket. |
| `--log-level` | `info` | Log verbosity. One of `debug`, `info`, `warn`, `error`. |
| `--max-arp-entries` | `10000` | Maximum number of ARP entries to export per scrape. Prevents metric cardinality explosion under ARP flooding. When exceeded, output is truncated and `network_arp_entries_truncated` is set to 1. |
| `--max-graph-edges` | `10000` | Maximum number of network graph edges to export per scrape. Prevents metric cardinality explosion under high connection volume. When exceeded, output is truncated and `network_graph_edges_truncated` is set to 1. |

## Collectors

### IPsec

The IPsec collector communicates with the strongSwan charon daemon over the
VICI protocol. On each scrape it issues two commands:

1. **`list-sas`** -- enumerates all IKE SAs and their child SAs, returning
   names, unique IDs, states, peer addresses, traffic selectors, and byte
   and packet counters.
2. **`stats`** -- retrieves charon daemon health metrics including uptime,
   worker thread counts, job queue depths, and half-open IKE SA counts.

#### Tunnel Auto-Discovery

Tunnels are discovered dynamically from the VICI response on every scrape.
When a new tunnel is established between scrapes, it appears in the next
scrape automatically. When a tunnel is removed or torn down, it disappears
from the next scrape. No exporter restart or configuration change is
required.

#### IKE SA States

The `ipsec_ike_sa_state` metric reports a numeric state value:

| Value | State | Description |
| --- | --- | --- |
| 0 | CREATED | SA allocated but not yet negotiating. |
| 1 | CONNECTING | IKE negotiation in progress. |
| 2 | ESTABLISHED | SA is up and operational. |
| 3 | PASSIVE | Passive SA (responder waiting). |
| 4 | REKEYING | Rekey in progress. |
| 5 | REKEYED | Old SA after a successful rekey (being replaced). |
| 6 | DELETING | SA is being torn down. |
| 7 | DESTROYING | SA is being destroyed. |

#### Child SA States

The `ipsec_child_sa_state` metric reports a numeric state value:

| Value | State | Description |
| --- | --- | --- |
| 0 | CREATED | Child SA allocated. |
| 1 | ROUTED | Trap policy installed, waiting for traffic. |
| 2 | INSTALLING | SPIs allocated, installing policies. |
| 3 | INSTALLED | SA is installed and passing traffic. |
| 4 | UPDATING | SA parameters are being updated. |
| 5 | REKEYING | Rekey in progress. |
| 6 | REKEYED | Old child SA after a successful rekey. |
| 7 | RETRYING | Retrying negotiation after a failure. |
| 8 | DELETING | Child SA is being deleted. |
| 9 | DELETED | Child SA has been deleted. |
| 10 | DESTROYING | Child SA is being destroyed. |

#### IPsec Metrics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `ipsec_up` | gauge | | Whether the VICI socket is reachable (1 = up, 0 = down). |
| `ipsec_ike_sas` | gauge | | Total number of IKE SAs. |
| `ipsec_half_open_ike_sas` | gauge | | Number of half-open IKE SAs (indicates potential DoS or connectivity issues). |
| `ipsec_ike_sa_state` | gauge | `name`, `uid`, `remote_host` | Numeric IKE SA state (0-7, see table above). |
| `ipsec_ike_sa_established_seconds` | gauge | `name`, `uid`, `remote_host` | Seconds since the IKE SA was established. |
| `ipsec_child_sa_state` | gauge | `ike_sa_name`, `name`, `uid`, `remote_host`, `local_ts`, `remote_ts` | Numeric child SA state (0-10, see table above). |
| `ipsec_child_sa_bytes_in` | gauge | `ike_sa_name`, `name`, `uid`, `remote_host`, `local_ts`, `remote_ts` | Bytes received on this child SA. |
| `ipsec_child_sa_bytes_out` | gauge | `ike_sa_name`, `name`, `uid`, `remote_host`, `local_ts`, `remote_ts` | Bytes sent on this child SA. |
| `ipsec_child_sa_packets_in` | gauge | `ike_sa_name`, `name`, `uid`, `remote_host`, `local_ts`, `remote_ts` | Packets received on this child SA. |
| `ipsec_child_sa_packets_out` | gauge | `ike_sa_name`, `name`, `uid`, `remote_host`, `local_ts`, `remote_ts` | Packets sent on this child SA. |
| `ipsec_child_sa_installed_seconds` | gauge | `ike_sa_name`, `name`, `uid`, `remote_host`, `local_ts`, `remote_ts` | Seconds since the child SA was installed. |
| `ipsec_uptime_seconds` | gauge | | Charon daemon uptime in seconds. |
| `ipsec_workers_total` | gauge | | Total number of charon worker threads. |
| `ipsec_idle_workers` | gauge | | Number of idle charon worker threads. |
| `ipsec_active_workers` | gauge | | Number of active charon worker threads. |
| `ipsec_queues` | gauge | `priority` | Number of queued jobs by priority (critical, high, medium, low). |

#### Label Reference

| Label | Appears On | Description |
| --- | --- | --- |
| `name` | IKE SA metrics | strongSwan connection name (e.g. `site-alpha`). |
| `uid` | IKE and child SA metrics | Unique ID assigned by charon. Changes on rekey. |
| `remote_host` | IKE and child SA metrics | IP address of the remote peer. |
| `ike_sa_name` | child SA metrics | Name of the parent IKE SA. |
| `local_ts` | child SA metrics | Local traffic selector (e.g. `203.0.113.10/32`). |
| `remote_ts` | child SA metrics | Remote traffic selector (e.g. `10.10.10.0/24`). |
| `priority` | `ipsec_queues` | Job queue priority level. |

### ARP

Reports the IPv4 ARP neighbor table. Each entry is a gauge with value 1,
labeled with IP address, MAC address, device name, and NUD state.

For full details, see the
[network_exporter documentation](network_exporter.md).

### Interface

Classifies network interfaces by type (physical, bond, bridge, vti, veth,
loopback) and reports bond and bridge membership relationships.

For full details, see the
[network_exporter documentation](network_exporter.md).

### Network Graph

Discovers network topology edges by examining active TCP and UDP connections
relative to local listening ports. Each edge is a gauge with value 1,
labeled with remote host, local port, and direction (inbound/outbound).

For full details, see the
[network_exporter documentation](network_exporter.md).

### Conntrack

Reports per-port connection counts by state, per-port byte counters from
the kernel conntrack table, and listening port presence.

For full details, see the
[network_exporter documentation](network_exporter.md).

### Firewall

Reports nftables DROP and REJECT packet and byte counters, as well as chain
default policy drop counters. Uses the kernel's nf_tables netlink subsystem
directly (no `nft` binary required).

For full details, see the
[network_exporter documentation](network_exporter.md).

## IPsec Details

### strongSwan VICI Protocol

The VICI (Versatile IKE Configuration Interface) protocol is strongSwan's
programmatic control interface. It uses a Unix domain socket (default path
`/var/run/charon.vici`) and a binary message format. The exporter uses the
[govici](https://github.com/strongswan/govici) Go library to communicate
with the daemon.

On each scrape the collector:

1. Dials the VICI socket.
2. Sends a `list-sas` streamed command request, receiving one message per
   IKE SA. Each message contains the IKE SA state, peer address,
   established duration, and a nested `child-sas` section with per-tunnel
   traffic counters and traffic selectors.
3. Sends a `stats` command request, receiving charon daemon health data
   including uptime, worker thread utilization, and job queue depths.
4. Closes the connection.

The connection is not held open between scrapes. Each scrape creates a
fresh VICI session, ensuring the exporter does not accumulate stale state
or prevent charon from cleaning up resources.

### SA Lifecycle and Rekeys

During an IKE SA rekey, strongSwan briefly maintains two IKE SAs for the
same connection: the old SA in state REKEYED (5) and the new SA in state
ESTABLISHED (2). Both appear in the metrics with distinct `uid` values.
The old SA disappears once charon completes the transition.

The `uid` label changes with every rekey. Use the `name` label to track a
logical tunnel across rekeys, and the `uid` label to distinguish individual
SA instances.

## Endpoints

| Path | Method | Description |
| --- | --- | --- |
| `/metrics` | GET | Prometheus metrics endpoint. |
| `/` | GET | Landing page with a link to `/metrics`. |

## Deployment

### Host Requirements

The exporter must run on a host that terminates IPsec tunnels via
strongSwan. The following must be available to the exporter process or
container:

- **VICI socket** (`/var/run/charon.vici`) -- bind-mount into the
  container as read-only.
- **procfs** -- required by the Network Graph and Conntrack collectors.
- **sysfs** -- required by the Interface collector.
- **Netlink access** -- required by the ARP, Conntrack, and Firewall
  collectors. Typically satisfied by `--network host` or `CAP_NET_ADMIN`.

### Kubernetes DaemonSet

Since the exporter collects host-level metrics, it runs as a DaemonSet on
nodes that terminate IPsec tunnels. Node selectors or node affinity rules
should restrict scheduling to IPsec hosts.

```yaml
apiVersion: apps/v1
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
            type: Socket
```

### Prometheus Scrape Config

```yaml
scrape_configs:
  - job_name: ipsec
    scrape_interval: 30s
    static_configs:
      - targets:
          - ipsec-host-1:9100
          - ipsec-host-2:9100
```

For Kubernetes with Prometheus service discovery:

```yaml
scrape_configs:
  - job_name: ipsec
    scrape_interval: 30s
    kubernetes_sd_configs:
      - role: pod
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_label_app]
        regex: ipsec-exporter
        action: keep
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_port]
        target_label: __address__
        regex: (.+)
        replacement: ${1}
```

## Failure Modes

| Scenario | Behavior |
| --- | --- |
| **VICI socket unavailable** | The IPsec collector emits `ipsec_up 0` and no other `ipsec_*` metrics. All network collectors continue to function normally. |
| **strongSwan not running** | Same as VICI socket unavailable. The collector detects this via `IsAvailable()` which attempts to dial the socket. |
| **`list-sas` succeeds but `stats` fails** | IKE and child SA metrics are emitted. Charon health metrics (`ipsec_uptime_seconds`, `ipsec_workers_total`, `ipsec_idle_workers`, `ipsec_active_workers`, `ipsec_queues`) are absent. `ipsec_up` remains 1. |
| **Partial collector failure** | The `ContinueOnError` handler ensures a failure in any one collector does not prevent other collectors from returning metrics. For example, if the Firewall collector cannot read nftables but the IPsec collector is healthy, IPsec metrics are still served. |
| **Tunnel flap / rekey** | Both old and new SAs appear with distinct `uid` values during the transition. The old SA disappears once charon completes the transition. |
| **Network collectors fail** | Network collector failures (e.g., procfs unreadable, netlink errors) do not affect the IPsec collector. Each collector is independent. |
