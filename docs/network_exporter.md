# Network Exporter

## Overview

`network_exporter` is a Prometheus exporter that collects host-level network
metrics and presents them on a standard `/metrics` endpoint. It is designed
to **complement, not replace** Alloy's embedded `node_exporter`: it exposes
the subset of network telemetry that `node_exporter` does not cover --
interface classification, ARP table state, per-port connection visibility,
network topology graphs, and firewall drop/reject counters -- while leaving
CPU, memory, disk, and other system metrics to the existing stack.

The exporter runs as a single container per host. It reads directly from
procfs, sysfs, and kernel netlink sockets, requiring no external binaries
or shell access. The runtime image is distroless.

## Architecture

The exporter follows a pull-based architecture. Each Prometheus scrape
triggers all registered collectors, which read live kernel state and return
metrics for that instant. There is no internal caching or aggregation store;
every scrape reflects the current state of the host.

### Collectors

| Collector | Package | Data Source | Description |
| --- | --- | --- | --- |
| **arp** | `src/collector/arp` | Netlink (`NETLINK_ROUTE`) | Full IPv4 ARP neighbor table with NUD state. |
| **iface** | `src/collector/iface` | sysfs (`/sys/class/net/`) | Interface type classification, bond membership, bridge membership. |
| **netgraph** | `src/collector/netgraph` | procfs (`/proc/net/tcp`, `/proc/net/udp`) | Deduplicated network topology edges between local listening ports and remote hosts. |
| **conntrack** | `src/collector/conntrack` | procfs (`/proc/net/tcp`, `/proc/net/udp`) + Netlink (`NETLINK_NETFILTER`, conntrack subsystem) | Per-port connection counts by state, listening port presence, and per-port byte counters from conntrack. |
| **tcpstate** | `src/collector/tcpstate` | procfs (`/proc/net/tcp`, `/proc/net/tcp6`) | Per-TCP-connection state with full endpoint labels. |
| **firewall** | `src/collector/firewall` | Netlink (`NETLINK_NETFILTER`, nf_tables subsystem) | Packet and byte counters for nftables DROP/REJECT rules and chain default DROP policies. |

### Collector Interface

Every collector implements the `collector.Collector` interface, which
extends `prometheus.Collector` with a `Name()` method used for logging
and registration diagnostics:

```go
type Collector interface {
    prometheus.Collector
    Name() string
}
```

### Data Sources

The exporter reads from three kernel interfaces:

- **procfs** -- `/proc/net/tcp`, `/proc/net/tcp6`, `/proc/net/udp`,
  `/proc/net/udp6` are parsed to discover active sockets and connections.
  The mount point is configurable via `--proc-path`.
- **sysfs** -- `/sys/class/net/` is read to classify network interfaces
  by type, driver, and master relationships. The mount point is
  configurable via `--sys-path`.
- **Netlink** -- The ARP collector uses `NETLINK_ROUTE` to read the
  neighbor table. The conntrack collector uses `NETLINK_NETFILTER`
  (conntrack subsystem) to read flow accounting data. The firewall
  collector uses `NETLINK_NETFILTER` (nf_tables subsystem) to read
  DROP/REJECT rule counters.

## Installation

Container images are published to GitHub Container Registry:

```
ghcr.io/phaseshiftdata/network_exporter
```

### Docker

```bash
docker run -d --rm \
  --name network_exporter \
  --network=host --pid=host \
  --user 0:0 \
  --cap-add=NET_ADMIN \
  --cap-add=DAC_READ_SEARCH \
  --security-opt label=disable \
  -v /proc:/host/proc:ro \
  -v /sys:/host/sys:ro \
  ghcr.io/phaseshiftdata/network_exporter:main \
  --proc-path=/host/proc \
  --sys-path=/host/sys \
  --listen-address=0.0.0.0:9100
```

### Host Requirements

The exporter needs access to the host's kernel state. It must run as
root (`--user 0:0`) because reading procfs entries of other processes and
querying nftables counters require root privileges. The container image
does not use file capabilities (`setcap`), so it is compatible with
hardened Docker environments that enable `userns-remap` and
`no-new-privileges`. Capabilities are provided at runtime via `cap_add`
and take effect because the process runs as root.

| Requirement | Reason |
| --- | --- |
| **procfs mount** (`/proc` -> `/host/proc`) | The netgraph and conntrack collectors parse `/proc/net/tcp` and `/proc/net/udp` to discover connections. |
| **sysfs mount** (`/sys` -> `/host/sys`) | The iface collector reads `/sys/class/net/` to classify interfaces. |
| **Host PID namespace** (`--pid=host`) | Required to see the host's `/proc/net/` rather than the container's. |
| **Host network namespace** (`--network=host`) | Required so netlink sockets operate in the host's network namespace. |
| **Root user** (`--user 0:0`) | Required for reading procfs entries of other processes and querying nftables counters. The default distroless UID 65532 is not sufficient. |
| **`CAP_NET_ADMIN`** | Required by `NETLINK_NETFILTER` for conntrack flow queries and nftables rule/counter reads. Also used by `NETLINK_ROUTE` for neighbor table access. |
| **`CAP_DAC_READ_SEARCH`** | Required for reading `/proc` entries of other processes. |
| **`--security-opt label=disable`** | Required on SELinux hosts to allow the container to read host procfs and sysfs mounts. |

## Configuration

All configuration is via CLI flags. No configuration file or environment
variables are required.

| Flag | Default | Description |
| --- | --- | --- |
| `--listen-address` | `127.0.0.1:9100` | Address and port the HTTP server listens on. |
| `--proc-path` | `/proc` | Path to procfs mount. Set to `/host/proc` when running in a container with the host's procfs bind-mounted. |
| `--sys-path` | `/sys` | Path to sysfs mount. Set to `/host/sys` when running in a container with the host's sysfs bind-mounted. |
| `--log-level` | `info` | Log verbosity. One of `debug`, `info`, `warn`, `error`. |
| `--max-arp-entries` | `10000` | Maximum number of ARP entries to export per scrape. Prevents metric cardinality explosion under ARP flooding. When exceeded, output is truncated and `network_arp_entries_truncated` is set to 1. |
| `--max-graph-edges` | `10000` | Maximum number of network graph edges to export per scrape. Prevents metric cardinality explosion under high connection volume. When exceeded, output is truncated and `network_graph_edges_truncated` is set to 1. |
| `--max-tcp-connections` | `10000` | Maximum number of per-connection TCP state metrics to export per scrape. Prevents metric cardinality explosion on busy hosts. When exceeded, output is truncated and `network_tcp_connections_truncated` is set to 1. |
| `--tcp-connection-states` | *(all states)* | Comma-separated list of TCP states to report (e.g. `ESTABLISHED,LISTEN,TIME_WAIT`). When empty, all states are reported. |

## Collectors

### ARP

The ARP collector reads the full IPv4 neighbor table via netlink and emits
one gauge per entry.

**Data source:** `NETLINK_ROUTE` neighbor table (equivalent to `ip neigh show`).

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `network_arp_entry` | gauge | `ip`, `mac`, `device`, `state` | ARP table entry; value is always 1. |
| `network_arp_entries_truncated` | gauge | *(none)* | Set to 1 when the ARP table exceeds `--max-arp-entries` and output is truncated; 0 otherwise. |

**Label values:**

- `ip` -- IPv4 address of the neighbor (e.g. `192.168.1.1`).
- `mac` -- MAC address of the neighbor (e.g. `aa:bb:cc:dd:ee:ff`).
- `device` -- Network interface the entry was learned on (e.g. `eth0`).
- `state` -- Kernel NUD state: `incomplete`, `reachable`, `stale`, `delay`,
  `probe`, `failed`, `noarp`, or `permanent`.

IPv6 entries and entries with nil IP addresses are skipped. Each scrape
reflects the current neighbor table; entries that disappear from the
kernel are absent from the next scrape.

### Interface

The interface collector classifies all network interfaces by type and
reports bond and bridge membership relationships.

**Data source:** sysfs at `<sys-path>/class/net/`.

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `network_interface_type` | gauge | `device`, `type`, `driver` | Interface type classification; value is always 1. |
| `network_bond_member` | gauge | `bond`, `member` | Bond membership; value is always 1. |
| `network_bridge_member` | gauge | `bridge`, `member` | Bridge membership; value is always 1. |

**Interface type classification:**

The `type` label is determined by reading sysfs attributes:

| Type Value | Detection Method |
| --- | --- |
| `bond` | `<device>/bonding` directory exists |
| `bridge` | `<device>/bridge` directory exists |
| `physical` | sysfs `type` file is `0` (ethernet) and name does not start with `veth` |
| `veth` | sysfs `type` file is `0` and name starts with `veth` |
| `vti` | sysfs `type` file is `768` (tunnel) |
| `loopback` | sysfs `type` file is `772` |
| `other` | None of the above |

The `driver` label is read from the `device/driver` symlink (e.g. `ixgbe`,
`bridge`, `bonding`). It is empty when the symlink does not exist.

**Membership metrics** are emitted for interfaces that have a `master`
symlink pointing to a bond or bridge device.

### Network Graph

The network graph collector discovers network topology edges by examining
active TCP and UDP connections relative to local listening ports.

**Data source:** procfs at `<proc-path>/net/tcp`, `<proc-path>/net/tcp6`,
`<proc-path>/net/udp`, `<proc-path>/net/udp6`.

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `network_graph_edge` | gauge | `remote_host`, `local_port`, `direction` | Presence indicator for a network topology edge; value is always 1. |
| `network_graph_edges_truncated` | gauge | *(none)* | Set to 1 when the edge count exceeds `--max-graph-edges` and output is truncated; 0 otherwise. |

**Label values:**

- `remote_host` -- IP address of the remote peer.
- `local_port` -- The service port. For inbound connections, this is the
  local listening port. For outbound connections, this is the remote
  service port.
- `direction` -- `inbound` when a remote host connects to a local
  listening port; `outbound` when this host connects to a remote service.

**Edge deduplication:** Multiple connections between the same remote host
and local port in the same direction produce a single metric. This keeps
cardinality proportional to the number of distinct (host, port, direction)
tuples rather than the number of individual connections.

**Loopback filtering:** Connections with loopback addresses (`127.0.0.1`,
`0.0.0.0`) on either side are excluded from the graph.

**Listening port detection:** A TCP socket in `LISTEN` state or a UDP
socket in `CLOSE` state with remote address `0.0.0.0` is treated as a
listening port. Connections to matching local ports are classified as
inbound; all others are outbound.

### Conntrack

The conntrack collector reports per-port connection counts, listening port
presence, and byte counters from the kernel's connection tracking subsystem.

**Data sources:**
- Sockets: procfs at `<proc-path>/net/tcp`, `<proc-path>/net/udp`
  (and their IPv6 counterparts).
- Flow accounting: `NETLINK_NETFILTER` conntrack subsystem (equivalent
  to `conntrack -L`).

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `network_port_connections` | gauge | `port`, `protocol`, `state` | Number of connections per port, protocol, and state. |
| `network_port_listen` | gauge | `port`, `protocol`, `bind_address` | Presence of a listening port; value is always 1. |
| `network_port_bytes_in` | gauge | `port`, `protocol` | Total inbound bytes per port from conntrack. |
| `network_port_bytes_out` | gauge | `port`, `protocol` | Total outbound bytes per port from conntrack. |
| `network_conntrack_accounting_enabled` | gauge | *(none)* | Whether conntrack accounting is available (1) or not (0). |

**Connection counting:** Only connections whose local port matches a
detected listening port are counted. The `state` label carries the TCP
state (e.g. `ESTABLISHED`, `TIME_WAIT`).

**Byte counters:** Conntrack flows are aggregated by destination port.
Only flows whose destination port matches a listening port are included.
When conntrack accounting is unavailable (kernel module not loaded, or no
`CAP_NET_ADMIN`), `network_conntrack_accounting_enabled` reads `0` and
byte counter metrics are omitted.

### TCP State

The TCP state collector reports per-TCP-connection state as individual
metrics, labeled with full endpoint information (local and peer address
and port).

**Data source:** procfs at `<proc-path>/net/tcp`, `<proc-path>/net/tcp6`.

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `network_tcp_connection` | gauge | `local_addr`, `local_port`, `peer_addr`, `peer_port`, `state` | Per-TCP-connection state indicator; value is always 1. |
| `network_tcp_connections_truncated` | gauge | *(none)* | Set to 1 when the connection count exceeds `--max-tcp-connections` and output is truncated; 0 otherwise. |

**Label values:**

- `local_addr` -- Local IP address (e.g. `192.168.1.10`).
- `local_port` -- Local port number (e.g. `443`).
- `peer_addr` -- Remote peer IP address (e.g. `10.0.0.5`).
- `peer_port` -- Remote peer port number (e.g. `52431`).
- `state` -- TCP connection state.

**TCP states:**

| Value | State |
| --- | --- |
| 01 | ESTABLISHED |
| 02 | SYN_SENT |
| 03 | SYN_RECV |
| 04 | FIN_WAIT1 |
| 05 | FIN_WAIT2 |
| 06 | TIME_WAIT |
| 07 | CLOSE |
| 08 | CLOSE_WAIT |
| 09 | LAST_ACK |
| 0A | LISTEN |
| 0B | CLOSING |

**Loopback filtering:** Connections where both local and remote addresses
are loopback (`127.0.0.1`, `0.0.0.0`, `::1`, `::`) are excluded.

**State filtering:** The `--tcp-connection-states` flag limits which TCP
states are reported. For example, `--tcp-connection-states=ESTABLISHED,LISTEN`
reports only established and listening connections.

**Cardinality cap:** The `--max-tcp-connections` flag limits the number of
connections reported per scrape. When exceeded, `network_tcp_connections_truncated`
is set to 1.

### Firewall

The firewall collector reports packet and byte counters for nftables
DROP and REJECT rules and chain default DROP policies.

**Data source:** `NETLINK_NETFILTER`, nf_tables subsystem (equivalent to
`nft list ruleset` with counters). The collector uses the
`github.com/google/nftables` library to speak the nf_tables netlink
protocol directly, requiring no `nft` binary in the container image.

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `network_firewall_collector_up` | gauge | *(none)* | Whether nftables counters could be read (1 = collecting, 0 = unavailable). |
| `network_firewall_drop_packets_total` | counter | `family`, `table`, `chain`, `rule` | Total packets dropped by nftables DROP rules. |
| `network_firewall_drop_bytes_total` | counter | `family`, `table`, `chain`, `rule` | Total bytes dropped by nftables DROP rules. |
| `network_firewall_reject_packets_total` | counter | `family`, `table`, `chain`, `rule` | Total packets rejected by nftables REJECT rules. |
| `network_firewall_reject_bytes_total` | counter | `family`, `table`, `chain`, `rule` | Total bytes rejected by nftables REJECT rules. |
| `network_firewall_policy_drop_packets_total` | counter | `family`, `table`, `chain` | Total packets dropped by chain default DROP policy. |
| `network_firewall_policy_drop_bytes_total` | counter | `family`, `table`, `chain` | Total bytes dropped by chain default DROP policy. |

**Label values:**

- `family` -- nftables table family: `ip`, `ip6`, `inet`, `bridge`,
  `arp`, or `netdev`.
- `table` -- nftables table name (e.g. `filter`).
- `chain` -- nftables chain name (e.g. `input`).
- `rule` -- Rule comment if available, otherwise the zero-indexed
  position of the rule in the chain.

**Probe behavior:** On startup, the collector probes whether the process
can open a `NETLINK_NETFILTER` socket and list chains. If this fails with
`EPERM`/`EACCES` (no `CAP_NET_ADMIN`) or `EPROTONOSUPPORT`/`EAFNOSUPPORT`
(no nf_tables in kernel), the failure is latched permanently and every
scrape reports `network_firewall_collector_up 0`. Transient errors (empty
ruleset, table vanishing between dumps) are not latched and recover on the
next scrape.

**Counter semantics:** Only rules that include a `counter` statement in
their nftables definition report non-zero values. Rules without counters
report zero, which accurately reflects the kernel's accounting behavior.

## Endpoints

| Path | Method | Description |
| --- | --- | --- |
| `/metrics` | GET | Prometheus metrics endpoint. |
| `/` | GET | Landing page with a link to `/metrics`. |

## Deployment

### Kubernetes DaemonSet

The exporter should run as a DaemonSet so every node in the cluster is
monitored. It requires host PID and network namespaces, plus procfs and
sysfs bind mounts.

```yaml
apiVersion: apps/v1
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
            path: /sys
```

### Prometheus Scrape Config

```yaml
scrape_configs:
  - job_name: network
    scrape_interval: 30s
    static_configs:
      - targets:
          - network-exporter:9100
```

### Docker Compose

```yaml
services:
  network_exporter:
    image: ghcr.io/phaseshiftdata/network_exporter:main
    pid: host
    network_mode: host
    user: "0:0"
    cap_add:
      - NET_ADMIN
      - DAC_READ_SEARCH
    security_opt:
      - label=disable
    volumes:
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
    command:
      - --proc-path=/host/proc
      - --sys-path=/host/sys
      - --listen-address=0.0.0.0:9100
    restart: unless-stopped
```

## Failure Modes

### ContinueOnError

The `/metrics` endpoint is configured with `promhttp.ContinueOnError`.
When a single collector fails to read its data source, the remaining
collectors still contribute their metrics. The failed collector's metric
families are absent from that scrape rather than the entire response
returning HTTP 500. This was a deliberate fix for an incident where a
host missing nftables reported nothing at all because the default
`HTTPErrorOnError` handling discarded the entire response.

### Missing procfs

If the procfs mount is not available or points to the wrong path, the
netgraph and conntrack collectors fail to parse `/proc/net/tcp` and
return an error. Their metric families are absent from the scrape, but
the ARP, interface, and firewall collectors continue to function.

### Missing sysfs

If the sysfs mount is not available, the interface collector fails to
read `/sys/class/net/` and returns an error. Its three metric families
(`network_interface_type`, `network_bond_member`, `network_bridge_member`)
are absent from the scrape, but all other collectors continue.

### nftables Not Available

If the kernel has no nf_tables support or the container lacks
`CAP_NET_ADMIN`, the firewall collector latches to a permanently-down
state at startup. Every scrape reports `network_firewall_collector_up 0`
and omits the drop/reject/policy counter metrics. This is logged once
at startup and does not produce repeated log entries.

### Conntrack Accounting Disabled

If conntrack accounting is not available (kernel module not loaded,
permissions insufficient, or conntrack flows cannot be listed), the
collector reports `network_conntrack_accounting_enabled 0` and omits
the `network_port_bytes_in` and `network_port_bytes_out` metrics.
Socket-based metrics (`network_port_connections`, `network_port_listen`)
continue to function because they read from procfs rather than netlink.
