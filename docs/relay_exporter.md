# Relay Exporter

## Overview

`relay_exporter` is a Prometheus metrics relay proxy for targets on
RFC 1918 private networks. Prometheus scrapes the relay, which fetches
metrics from targets behind VPN tunnels or private networks that
Prometheus cannot directly reach. The relay validates every request,
enforces source IP filtering, and restricts targets to private address
ranges so it cannot be used as an open proxy.

Three proxy endpoints are available:

| Endpoint | Proxies to | Returns |
| --- | --- | --- |
| `/metrics` | `/metrics` | Target's own telemetry (e.g. Alloy self-metrics) |
| `/host` | `/api/v0/component/prometheus.exporter.unix.host/metrics` | Host metrics (`node_*`) from Grafana Alloy |
| `/cadvisor` | `/api/v0/component/prometheus.exporter.cadvisor.containers/metrics` | Container metrics (`container_*`) from Grafana Alloy |

All three endpoints share the same request contract, validation rules,
and response format. The only difference is the constant path appended
to the target URL.

The relay always returns HTTP 200 to Prometheus when it is functioning
correctly. The target's actual HTTP status and reachability are reported
via gauge metrics appended to the response body.

## Architecture

```
Prometheus --> relay_exporter --> target (RFC 1918 host)
                (network A)         (network B)
```

1. Prometheus sends an HTTP(S) GET to `relay_exporter` at `/metrics`,
   `/host`, or `/cadvisor` with query parameters:
   `/<endpoint>?ip=<target_ip>&port=<number>&tls=<true|false>`
2. `relay_exporter` validates the request (source IP, target IP, port).
3. `relay_exporter` proxies the request to the target at the constant
   path for that endpoint (e.g. `http(s)://<ip>:<port>/metrics` for
   `/metrics`, or the Alloy component path for `/host` and `/cadvisor`).
4. `relay_exporter` returns HTTP 200 with the target's response body and
   relay status metrics appended.

## Installation

Container images are published to GitHub Container Registry:

```
ghcr.io/phaseshiftdata/relay_exporter
```

### Docker

```bash
docker run -d --rm \
  --name relay_exporter \
  -p 9100:9100 \
  ghcr.io/phaseshiftdata/relay_exporter:main \
  --listen-address=0.0.0.0:9100 \
  --allowed-source=203.0.113.10
```

## Configuration

All configuration is via CLI flags. No configuration file or environment
variables are required.

| Flag | Default | Description |
| --- | --- | --- |
| `--listen-address` | `127.0.0.1:9100` | Address and port the HTTP server listens on. |
| `--allowed-source` | *(required)* | Single IP address allowed to send scrape requests. The relay refuses to start if omitted. |
| `--tls-cert-file` | *(optional)* | TLS certificate for the relay listener. When provided with `--tls-key-file`, the relay serves HTTPS. |
| `--tls-key-file` | *(optional)* | TLS private key for the relay listener. When provided with `--tls-cert-file`, the relay serves HTTPS. |
| `--ca-cert` | *(optional)* | CA certificate bundle for verifying target TLS certificates. |
| `--tls-skip-verify` | `false` | Skip TLS certificate verification when connecting to targets. |
| `--proxy-timeout` | `10s` | Timeout for proxy requests to targets. |
| `--concurrent-requests` | `100` | Maximum number of concurrent proxy requests. |
| `--log-level` | `info` | Log verbosity. One of `debug`, `info`, `warn`, `error`. |

## Request Validation

### Source IP Filtering (HTTP 403)

Only the IP address specified by `--allowed-source` may send requests.
All other source IPs receive HTTP 403 Forbidden. The `--allowed-source`
flag is **required** -- `relay_exporter` refuses to start with a clear
error message if it is omitted.

### Query Parameter Validation (HTTP 400)

- `ip` is **required** and must be a valid RFC 1918 address:
  - `10.0.0.0/8`
  - `172.16.0.0/12`
  - `192.168.0.0/16`
- `port` is **required** and must be 1-65535 (port 0 is explicitly
  disallowed).
- `tls` is **optional**, defaults to `false`, must be exactly `true`
  or `false`.
- Missing or invalid `ip`, `port`, or `tls` returns HTTP 400 Bad Request.

## Proxy Behavior

When validation passes, `relay_exporter` sends an HTTP GET to the
target on behalf of Prometheus. The target path is a constant per
endpoint:

- `/metrics` proxies to `http(s)://<ip>:<port>/metrics`
- `/host` proxies to `http(s)://<ip>:<port>/api/v0/component/prometheus.exporter.unix.host/metrics`
- `/cadvisor` proxies to `http(s)://<ip>:<port>/api/v0/component/prometheus.exporter.cadvisor.containers/metrics`

- HTTPS is used for the target connection when `tls=true`.
- The `Authorization` header from the Prometheus request is forwarded
  to the target if present.
- Target TLS verification uses system CAs by default, `--ca-cert`
  for a custom CA bundle, or `--tls-skip-verify` to skip verification.

## Response Format

Responses use standard Prometheus text format with `# HELP` and
`# TYPE` headers. The relay appends three gauge metrics after the
target's response body.

### Successful Target Response

```
# (target's /metrics body verbatim)

# HELP relay_response Whether the relay itself is functioning
# TYPE relay_response gauge
relay_response 1
# HELP relay_target_response Whether the target responded successfully
# TYPE relay_target_response gauge
relay_target_response 1
# HELP relay_target_http_status HTTP status code returned by the target
# TYPE relay_target_http_status gauge
relay_target_http_status 200
```

### Target Timeout or Connection Failure

```
# HELP relay_response Whether the relay itself is functioning
# TYPE relay_response gauge
relay_response 1
# HELP relay_target_response Whether the target responded successfully
# TYPE relay_target_response gauge
relay_target_response 0
# HELP relay_target_http_status HTTP status code returned by the target
# TYPE relay_target_http_status gauge
relay_target_http_status 0
```

## HTTP Status Codes

| Scenario | Status Returned to Prometheus | `relay_target_response` | `relay_target_http_status` |
| --- | --- | --- | --- |
| Target responded with metrics | **200** | 1 | 200 |
| Target component path returned 404 (`/host`, `/cadvisor`) | **200** | 0 | 404 |
| Target timed out or unreachable | **200** | 0 | 0 |
| Source IP not allowed | **403** | -- | -- |
| Missing or invalid query parameters | **400** | -- | -- |
| Concurrent request limit exceeded | **429** | -- | -- |

The relay always returns HTTP 200 to Prometheus when it is functioning
correctly. The target's actual HTTP status is reported via
`relay_target_http_status` in the response body.

## TLS

### Relay Listener TLS (Optional)

TLS for the relay listener is optional. If `--tls-cert-file` and
`--tls-key-file` are both provided, the relay serves HTTPS. Otherwise
it serves HTTP.

### Target Connection TLS

When `tls=true` is passed as a query parameter, the relay connects to
the target over HTTPS. Target certificate verification behavior:

- **Default:** system CA bundle.
- **Custom CA:** provide `--ca-cert` with a path to a CA certificate
  bundle.
- **Skip verification:** set `--tls-skip-verify` to skip TLS
  certificate verification when connecting to targets.

## Prometheus Configuration

The relay uses the multi-target exporter pattern. Prometheus discovers
targets and passes their address to the relay via relabel configuration.

```yaml
scrape_configs:
  - job_name: relay
    scrape_interval: 30s
    static_configs:
      - targets:
          - 10.0.0.1:9100
          - 10.0.0.2:9100
          - 192.168.1.10:9100
    relabel_configs:
      - source_labels: [__address__]
        regex: "([^:]+):(\\d+)"
        target_label: __param_ip
        replacement: "${1}"
      - source_labels: [__address__]
        regex: "([^:]+):(\\d+)"
        target_label: __param_port
        replacement: "${2}"
      - source_labels: [__address__]
        target_label: instance
      - target_label: __address__
        replacement: "203.0.113.5:9100"
```

In this configuration, Prometheus sends all scrape requests to the
relay at `203.0.113.5:9100`, which proxies them to the private targets
listed in `static_configs`.

### Host Metrics via `/host`

```yaml
scrape_configs:
  - job_name: edge_host
    scrape_interval: 30s
    metrics_path: /host
    static_configs:
      - targets:
          - 10.0.0.1:9100
        labels:
          site: example
    relabel_configs:
      - source_labels: [__address__]
        regex: "([^:]+):(\\d+)"
        target_label: __param_ip
        replacement: "${1}"
      - source_labels: [__address__]
        regex: "([^:]+):(\\d+)"
        target_label: __param_port
        replacement: "${2}"
      - source_labels: [__address__]
        target_label: instance
      - target_label: __address__
        replacement: "203.0.113.5:9100"
```

### Container Metrics via `/cadvisor`

```yaml
scrape_configs:
  - job_name: edge_cadvisor
    scrape_interval: 30s
    metrics_path: /cadvisor
    static_configs:
      - targets:
          - 10.0.0.1:9100
        labels:
          site: example
    relabel_configs:
      - source_labels: [__address__]
        regex: "([^:]+):(\\d+)"
        target_label: __param_ip
        replacement: "${1}"
      - source_labels: [__address__]
        regex: "([^:]+):(\\d+)"
        target_label: __param_port
        replacement: "${2}"
      - source_labels: [__address__]
        target_label: instance
      - target_label: __address__
        replacement: "203.0.113.5:9100"
```

**Note:** Each endpoint emits the same `relay_*` gauge metrics. If the
same device is scraped via `/metrics`, `/host`, and `/cadvisor` with
identical labels, the `relay_*` series will collide. Use distinct
`job_name` values per endpoint (as shown above) to avoid this.

## Endpoints

| Path | Method | Description |
| --- | --- | --- |
| `/metrics?ip=...&port=...&tls=...` | GET | Proxy endpoint. Fetches `/metrics` from the target and returns the response with relay status metrics appended. |
| `/host?ip=...&port=...&tls=...` | GET | Proxy endpoint. Fetches Grafana Alloy host metrics (`node_*`) from the target's `prometheus.exporter.unix.host` component. |
| `/cadvisor?ip=...&port=...&tls=...` | GET | Proxy endpoint. Fetches Grafana Alloy container metrics (`container_*`) from the target's `prometheus.exporter.cadvisor.containers` component. |
| `/health` | GET | Health and readiness check for liveness probes. |
| `/` | GET | Landing page with links to other endpoints. |

## Deployment

### Kubernetes

```yaml
apiVersion: apps/v1
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
      targetPort: 9100
```

### Docker Compose

```yaml
services:
  relay_exporter:
    image: ghcr.io/phaseshiftdata/relay_exporter:main
    ports:
      - "9100:9100"
    command:
      - --listen-address=0.0.0.0:9100
      - --allowed-source=203.0.113.10
    restart: unless-stopped
```

## Failure Modes

### Target Unreachable

When the target is unreachable (host down, network partition, DNS
failure), the relay returns HTTP 200 with `relay_target_response 0`
and `relay_target_http_status 0`. The `relay_response 1` metric
confirms the relay itself is functioning.

### Proxy Timeout

When the target does not respond within the `--proxy-timeout` window
(default 10s), the relay treats it the same as an unreachable target:
HTTP 200 with `relay_target_response 0` and `relay_target_http_status 0`.

### Concurrent Request Limit

When the number of in-flight proxy requests exceeds `--concurrent-requests`
(default 100), the relay returns HTTP 429 Too Many Requests. This
prevents resource exhaustion when many targets are scraped simultaneously.

## Security

- **Source IP filtering:** Only the IP specified by `--allowed-source`
  may send requests. All other sources receive HTTP 403.
- **RFC 1918 restriction:** The relay only proxies requests to private
  IP addresses (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`).
  Public IP addresses are rejected with HTTP 400.
- **No open proxy:** The combination of source IP filtering, RFC 1918
  target restriction, and fixed target paths (constants per endpoint)
  prevents the relay from being used as an open proxy. There is no
  user-controlled path parameter.
- **Authorization forwarding:** The `Authorization` header is forwarded
  from Prometheus to the target, supporting bearer token authentication
  on targets.
- **Response body limit:** Target responses are capped at 100 MiB. This
  prevents a misbehaving target from causing the relay to exhaust memory.
  Prometheus scrape responses are typically well under 1 MiB.

## Logging

- Proxied requests are logged to stdout at **debug** level with target,
  status, and duration.
- At the default `info` level, only startup, shutdown, and errors are
  logged.
