# OpenBao Exporter

## Overview

`openbao_exporter` is a Prometheus exporter for OpenBao cluster metrics.
It connects to a single OpenBao seed node, collects health status and
native metrics, discovers cluster members via raft configuration, and
re-exposes everything at `/metrics` in standard Prometheus text format.

OpenBao exposes metrics at `/v1/sys/metrics?format=prometheus` and health
at `/v1/sys/health`, but neither is at the standard `/metrics` path. This
exporter bridges that gap, making OpenBao metrics scrapable by standard
Prometheus or relay_exporter configurations.

## Architecture

```
Prometheus --> openbao_exporter --> OpenBao seed node
                                --> OpenBao node 2 (discovered)
                                --> OpenBao node 3 (discovered)
```

1. On startup, the exporter connects to the configured seed node.
2. It periodically discovers cluster members via
   `/v1/sys/storage/raft/configuration` (if available).
3. On each scrape, it fetches `/v1/sys/health` for node status and
   `/v1/sys/metrics?format=prometheus` for native metrics.
4. All metrics are combined and served at `/metrics`.

## Installation

Container images are published to GitHub Container Registry:

```
ghcr.io/phaseshiftdata/openbao_exporter
```

### Docker

```bash
docker run -d --rm \
  --name openbao_exporter \
  -p 9100:9100 \
  ghcr.io/phaseshiftdata/openbao_exporter:main \
  --listen-address=0.0.0.0:9100 \
  --openbao-addr=https://openbao:8200
```

### With Authentication

```bash
docker run -d --rm \
  --name openbao_exporter \
  -p 9100:9100 \
  -v /path/to/token:/token:ro \
  ghcr.io/phaseshiftdata/openbao_exporter:main \
  --listen-address=0.0.0.0:9100 \
  --openbao-addr=https://openbao:8200 \
  --openbao-token-file=/token
```

## Configuration

All configuration is via CLI flags. No configuration file or environment
variables are required.

| Flag | Default | Description |
| --- | --- | --- |
| `--listen-address` | `127.0.0.1:9100` | Address and port the HTTP server listens on. |
| `--openbao-addr` | *(required)* | OpenBao API address (e.g., `https://openbao:8200`). |
| `--openbao-token` | *(optional)* | Authentication token for OpenBao API. |
| `--openbao-token-file` | *(optional)* | Path to file containing authentication token. |
| `--log-level` | `info` | Log verbosity. One of `debug`, `info`, `warn`, `error`. |
| `--poll-interval` | `30s` | How often to re-discover cluster members. |

## Metrics

### Health Metrics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `openbao_up` | gauge | | 1 if the seed node is reachable. |
| `openbao_initialized` | gauge | | 1 if the cluster is initialized. |
| `openbao_sealed` | gauge | `node` | 1 if the node is sealed. |
| `openbao_standby` | gauge | `node` | 1 if the node is in standby mode. |
| `openbao_leader` | gauge | `node` | 1 on the leader node. |
| `openbao_node_info` | gauge | `node`, `version` | Information about an OpenBao node. |

### Cluster Metrics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `openbao_raft_committed_index` | gauge | | Raft committed index. |
| `openbao_raft_applied_index` | gauge | | Raft applied index. |
| `openbao_peers` | gauge | | Number of raft peers. |

### Native Metrics

All metrics from `/v1/sys/metrics?format=prometheus` are passed through.
These include runtime statistics, storage backend metrics, and other
internal OpenBao telemetry.

## OpenBao Health Endpoint

The `/v1/sys/health` endpoint returns different HTTP status codes for
different node states, but always returns a JSON body:

| Status Code | Node State |
| --- | --- |
| 200 | Active (initialized, unsealed, leader) |
| 429 | Standby |
| 472 | DR secondary standby |
| 501 | Uninitialized |
| 503 | Sealed |

The exporter treats all of these as valid responses and parses the JSON
body regardless of status code.

## Cluster Discovery

The exporter discovers cluster members via
`/v1/sys/storage/raft/configuration`. This endpoint:

- Requires authentication (an `--openbao-token` or `--openbao-token-file`).
- May not be available on all deployments (e.g., non-raft storage backends).
- Returns 403 if the token lacks permission, or 404 if raft is not in use.

When cluster discovery is unavailable, the exporter reports only the seed
node's health metrics. The `openbao_peers` metric defaults to 1 in this case.

## Prometheus Configuration

```yaml
scrape_configs:
  - job_name: openbao
    scrape_interval: 30s
    static_configs:
      - targets:
          - openbao-exporter:9100
```

## Endpoints

| Path | Method | Description |
| --- | --- | --- |
| `/metrics` | GET | Prometheus metrics endpoint. |
| `/` | GET | Landing page with link to metrics. |

## Deployment

### Docker Compose

```yaml
services:
  openbao_exporter:
    image: ghcr.io/phaseshiftdata/openbao_exporter:main
    ports:
      - "9100:9100"
    command:
      - --listen-address=0.0.0.0:9100
      - --openbao-addr=https://openbao:8200
      - --openbao-token-file=/run/secrets/openbao_token
    secrets:
      - openbao_token
    restart: unless-stopped
```

### Kubernetes

```yaml
apiVersion: apps/v1
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
            - --openbao-token-file=/etc/openbao/token
          ports:
            - containerPort: 9100
          volumeMounts:
            - name: token
              mountPath: /etc/openbao
              readOnly: true
      volumes:
        - name: token
          secret:
            secretName: openbao-token
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
      targetPort: 9100
```

## Failure Modes

### Seed Node Unreachable

When the seed node is unreachable, the exporter reports `openbao_up 0`
and no other metrics. The `/metrics` endpoint still returns HTTP 200.

### Authentication Failure

When the token is invalid or missing, health metrics are still collected
(the health endpoint does not require authentication), but native metrics
and cluster discovery may fail. The exporter logs these failures at debug
level and continues operating with reduced functionality.

### Raft Not Available

When the storage backend is not raft (e.g., file or Consul), cluster
discovery returns 404 and the exporter reports only the seed node with
`openbao_peers 1`.

## Security

- **Token handling:** The authentication token is stored as `[]byte` and
  zeroed on shutdown to minimize exposure in memory.
- **Token file:** Supports file-based token injection (e.g., Kubernetes
  secrets, Docker secrets) to avoid passing tokens via command-line flags.
- **Distroless container:** The runtime image is
  `gcr.io/distroless/static-debian12:nonroot` with no shell, no package
  manager, and runs as UID 65532.
- **Response body limit:** All API responses are capped at 100 MiB to
  prevent memory exhaustion from malformed responses.

## Logging

- Proxied requests are logged to stderr at **debug** level.
- At the default `info` level, only startup, shutdown, and errors are
  logged.
