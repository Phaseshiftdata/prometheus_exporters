# Libvirt Exporter

## Overview

`libvirt_exporter` is a Prometheus exporter for libvirtd. It collects
hypervisor-level and per-virtual-machine metrics via the libvirt API,
covering CPU, memory, block I/O, and network I/O for every domain managed
by the hypervisor.

The exporter connects to the libvirt daemon on every scrape and enumerates
all domains (VMs). Domains are auto-discovered; no static configuration of
VM names or UUIDs is required.

## Architecture

The exporter registers a single collector into a Prometheus registry. The
collector connects to libvirtd via its Unix socket (or optionally TCP) using
Go libvirt bindings that require CGO. On each scrape the collector:

1. Connects to the libvirt daemon.
2. Queries hypervisor-level information (host CPU count, total memory, free
   memory).
3. Lists all domains and, for each one, collects domain info (state, memory,
   vCPUs, CPU time), memory statistics, block device statistics, and network
   interface statistics.
4. Closes the connection.

The connection is not held open between scrapes. Each scrape creates a fresh
libvirt session, ensuring the exporter does not accumulate stale state.

The HTTP handler is configured with `ContinueOnError` so that a failure
collecting statistics for an individual domain does not prevent metrics from
being returned for the remaining domains.

## Installation

Container images are published to GitHub Container Registry:

```
ghcr.io/phaseshiftdata/libvirt_exporter
```

### Docker

```bash
docker run -d \
  --name libvirt_exporter \
  -v /var/run/libvirt/libvirt-sock:/var/run/libvirt/libvirt-sock:ro \
  -p 9177:9177 \
  ghcr.io/phaseshiftdata/libvirt_exporter:main \
  --listen-address=0.0.0.0:9177
```

### Host Requirements

The container requires access to the libvirt daemon:

| Resource | Mount | Purpose |
| --- | --- | --- |
| libvirt socket | `/var/run/libvirt/libvirt-sock` | Domain enumeration and metric collection |

The libvirtd service must be running on the host and the socket must be
accessible (readable) by the container's user. If the socket is
permission-restricted, run the container as root or add the container user
to the `libvirt` group.

## Configuration

All configuration is via CLI flags. No configuration file or environment
variables are required.

| Flag | Default | Description |
| --- | --- | --- |
| `--listen-address` | `127.0.0.1:9177` | Address and port the HTTP server listens on. |
| `--libvirt-uri` | `qemu:///system` | Libvirt connection URI. Use `qemu:///system` for the system daemon or `qemu+tcp://host/system` for remote TCP connections. |
| `--log-level` | `info` | Log verbosity. One of `debug`, `info`, `warn`, `error`. |

## Metrics Reference

### Hypervisor Metrics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `libvirt_up` | gauge | | Whether the libvirt daemon is reachable (1 = up, 0 = down). |
| `libvirt_domains_total` | gauge | | Total number of defined domains (VMs). |
| `libvirt_host_cpu_count` | gauge | | Number of physical CPUs on the hypervisor host. |
| `libvirt_host_memory_bytes` | gauge | | Total physical memory on the hypervisor host in bytes. |
| `libvirt_host_free_memory_bytes` | gauge | | Free physical memory on the hypervisor host in bytes. |

### Per-Domain Metrics

All per-domain metrics carry the labels `domain` (VM name) and `uuid`
(libvirt UUID).

#### Domain Info

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `libvirt_domain_info_state` | gauge | `domain`, `uuid` | Numeric domain state (see Domain States table). |
| `libvirt_domain_info_max_memory_bytes` | gauge | `domain`, `uuid` | Maximum allowed memory for the domain in bytes. |
| `libvirt_domain_info_memory_bytes` | gauge | `domain`, `uuid` | Current memory allocation for the domain in bytes. |
| `libvirt_domain_info_vcpus` | gauge | `domain`, `uuid` | Number of virtual CPUs assigned to the domain. |
| `libvirt_domain_cpu_time_seconds_total` | counter | `domain`, `uuid` | Total CPU time consumed by the domain in seconds. |

#### Memory Statistics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `libvirt_domain_memory_stats_bytes` | gauge | `domain`, `uuid`, `stat` | Memory statistics for the domain. The `stat` label identifies the statistic (e.g. `actual`, `rss`, `unused`, `available`, `usable`, `swap_in`, `swap_out`, `major_fault`, `minor_fault`). |

#### Block Device Statistics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `libvirt_domain_block_read_bytes_total` | counter | `domain`, `uuid`, `device` | Total bytes read from the block device. |
| `libvirt_domain_block_read_requests_total` | counter | `domain`, `uuid`, `device` | Total read requests issued to the block device. |
| `libvirt_domain_block_write_bytes_total` | counter | `domain`, `uuid`, `device` | Total bytes written to the block device. |
| `libvirt_domain_block_write_requests_total` | counter | `domain`, `uuid`, `device` | Total write requests issued to the block device. |

#### Network Interface Statistics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `libvirt_domain_net_receive_bytes_total` | counter | `domain`, `uuid`, `interface` | Total bytes received on the network interface. |
| `libvirt_domain_net_receive_packets_total` | counter | `domain`, `uuid`, `interface` | Total packets received on the network interface. |
| `libvirt_domain_net_receive_errors_total` | counter | `domain`, `uuid`, `interface` | Total receive errors on the network interface. |
| `libvirt_domain_net_transmit_bytes_total` | counter | `domain`, `uuid`, `interface` | Total bytes transmitted on the network interface. |
| `libvirt_domain_net_transmit_packets_total` | counter | `domain`, `uuid`, `interface` | Total packets transmitted on the network interface. |
| `libvirt_domain_net_transmit_errors_total` | counter | `domain`, `uuid`, `interface` | Total transmit errors on the network interface. |

## Domain States

The `libvirt_domain_info_state` metric reports a numeric state value
corresponding to the libvirt domain lifecycle:

| Value | State | Description |
| --- | --- | --- |
| 1 | RUNNING | The domain is actively running. |
| 2 | BLOCKED | The domain is blocked on a resource (waiting for I/O). |
| 3 | PAUSED | The domain has been paused by the user. |
| 4 | SHUTDOWN | The domain is in the process of shutting down. |
| 5 | SHUTOFF | The domain is shut off (not running). |
| 6 | CRASHED | The domain has crashed. |
| 7 | SUSPENDED | The domain is suspended (saved to disk). |

## Endpoints

| Path | Method | Description |
| --- | --- | --- |
| `/metrics` | GET | Prometheus metrics endpoint. |
| `/` | GET | Landing page with a link to `/metrics`. |

## Deployment

### Kubernetes DaemonSet

Since the exporter collects hypervisor-level metrics, it runs as a DaemonSet
on nodes that run libvirtd. Node selectors or node affinity rules should
restrict scheduling to hypervisor hosts.

```yaml
apiVersion: apps/v1
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
            type: Socket
```

### Prometheus Scrape Config

```yaml
scrape_configs:
  - job_name: libvirt
    scrape_interval: 30s
    static_configs:
      - targets:
          - hypervisor-1:9177
          - hypervisor-2:9177
```

### Docker Compose

```yaml
services:
  libvirt-exporter:
    image: ghcr.io/phaseshiftdata/libvirt_exporter:main
    command:
      - --listen-address=0.0.0.0:9177
    ports:
      - "9177:9177"
    volumes:
      - /var/run/libvirt/libvirt-sock:/var/run/libvirt/libvirt-sock:ro
    restart: unless-stopped
```

## Failure Modes

| Scenario | Behavior |
| --- | --- |
| **libvirtd unreachable** | The collector emits `libvirt_up 0` and no other `libvirt_*` metrics. The scrape still returns HTTP 200. |
| **Individual domain stat failure** | The `ContinueOnError` handler ensures that a failure collecting stats for one domain (e.g., a domain that was destroyed mid-scrape) does not prevent metrics for other domains from being returned. |
| **Socket permissions** | If the exporter process cannot open the libvirt socket due to file permissions, the collector emits `libvirt_up 0`. Run the container as root or ensure the container user belongs to the `libvirt` group on the host. |
| **Domain destroyed mid-scrape** | If a domain is destroyed between the list call and the stats call, the stats call for that domain fails gracefully. Metrics for all other domains are unaffected. |

## CGO Note

Unlike the other exporters in this repository, `libvirt_exporter` requires
CGO because the Go libvirt bindings link against the native `libvirt-dev` C
library. This means:

- The container image cannot use a distroless or scratch base image. It uses
  a minimal base that includes the `libvirt` shared libraries.
- Cross-compilation requires a C cross-compiler and the target platform's
  `libvirt-dev` headers.
- The build stage in the Dockerfile installs `libvirt-dev` before running
  `go build`.
