# Cloudflare Exporter

## Overview

`cloudflare_exporter` is a Prometheus exporter that collects metrics from the
Cloudflare API (GraphQL Analytics and REST) and presents them on a standard
`/metrics` endpoint. It is designed to **complement, not replace** the
Cloudflare dashboard: it exposes the subset of Cloudflare telemetry that is
useful for alerting and capacity planning inside a Prometheus/Grafana stack,
while leaving full-fidelity log analysis to Cloudflare's own tools.

The exporter runs as a single stateless container. It discovers the accounts,
zones, and products available to the configured API token, then schedules
collection cycles that stay within Cloudflare's API rate limits.

## Architecture

### Capability Discovery

On startup (and at a configurable interval thereafter) the exporter inspects
the API token's permissions and the account's entitled products. Only
collectors whose prerequisites are satisfied are activated. This means a
single binary works for Free plans (REST-only floor) through Enterprise
(full GraphQL Analytics access) without manual feature flags.

### Scheduler

A central scheduler triggers each enabled collector at the configured
refresh interval (`CF_REFRESH_INTERVAL_SECONDS`). Collectors are staggered
to spread API calls over time rather than bursting at the top of each cycle.

### Quota Governor

Two token-bucket governors independently track GraphQL and REST call budgets
(`CF_GRAPHQL_BUDGET_PER_WINDOW` and `CF_REST_BUDGET_PER_WINDOW`). When a
budget is exhausted the scheduler delays the next collection cycle rather
than returning errors; metrics remain stale-but-served until the budget
refills.

### Aggregation Store

Each collector writes results into an in-memory aggregation store. The store
holds exactly one generation of data per metric family. When Prometheus
scrapes `/metrics`, the store serializes the current generation. There is no
persistent state; a restart rebuilds the store from the next collection
cycle.

## Installation

Container images are published to GitHub Container Registry:

```
ghcr.io/phaseshiftdata/cloudflare_exporter
```

### Docker

```bash
docker run -d \
  --name cloudflare_exporter \
  -p 9199:9199 \
  -e CF_API_TOKEN="your-cloudflare-api-token" \
  ghcr.io/phaseshiftdata/cloudflare_exporter:main
```

### Docker Compose

```yaml
services:
  cloudflare_exporter:
    image: ghcr.io/phaseshiftdata/cloudflare_exporter:main
    ports:
      - "9199:9199"
    environment:
      CF_API_TOKEN: "${CF_API_TOKEN}"
    restart: unless-stopped
```

## Configuration

All configuration is via environment variables or CLI flags. No
configuration file is required.

### File-Based Secrets

For credentials that should not appear in process argument lists or
environment variables, the exporter supports reading secrets from files.
Each credential flag has a `-file` variant:

| Flag | File Variant | Description |
| --- | --- | --- |
| `--cf.api-token` | `--cf.api-token-file` | Cloudflare API token |
| `--web.basic-auth-password` | `--web.basic-auth-password-file` | Basic auth password |

The file is read at startup. Trailing whitespace and newlines are trimmed.
The exporter exits with an error if the file is missing, unreadable, or
empty after trimming. Setting both the value flag and its `-file` variant
is a configuration error.

Example:

```bash
echo -n "your-cloudflare-token" > /run/secrets/cf_api_token
cloudflare_exporter --cf.api-token-file /run/secrets/cf_api_token
```

### Environment Variables

| Variable | Default | Description |
| --- | --- | --- |
| `CF_API_TOKEN` | *(required)* | Cloudflare API token with appropriate permissions. |
| `CF_ACCOUNTS` | *(auto-discovered)* | Comma-separated list of account IDs to monitor. When omitted, all accounts visible to the token are used. |
| `CF_ZONES` | *(auto-discovered)* | Comma-separated list of zone IDs to monitor. When omitted, all zones visible to the token are used. |
| `CF_ZONES_EXCLUDE` | *(empty)* | Comma-separated list of zone IDs to exclude from monitoring. |
| `CF_SCRAPE_DELAY_SECONDS` | `300` | Propagation delay (seconds) subtracted from "now" when querying analytics. Cloudflare's analytics pipeline has roughly a five-minute lag; setting this lower may return incomplete data. |
| `CF_TIME_WINDOW_SECONDS` | `60` | Width of the query window (seconds) for each collection cycle. |
| `CF_REFRESH_INTERVAL_SECONDS` | `60` | How often (seconds) the scheduler triggers a new collection cycle. |
| `CF_DISCOVERY_INTERVAL_SECONDS` | `21600` | How often (seconds) capability discovery re-runs to pick up new zones or product entitlements. Default is six hours. |
| `CF_GRAPHQL_BUDGET_PER_WINDOW` | `160` | Maximum GraphQL API calls allowed per five-minute sliding window. |
| `CF_REST_BUDGET_PER_WINDOW` | `600` | Maximum REST API calls allowed per five-minute sliding window. |
| `CF_COLLECTORS_ENABLED` | *(all discovered)* | Comma-separated list of collector names to enable. When omitted, all collectors whose capabilities are satisfied are enabled. |
| `CF_GATEWAY_CATEGORY_ALLOWLIST` | *(empty)* | Comma-separated allowlist of Gateway content categories to track. When set, only these categories appear in `gateway_dns_queries_total` labels. |
| `CF_GATEWAY_CATEGORY_TOP_N` | `25` | Maximum number of top Gateway categories to retain per cycle (limits label cardinality). |
| `CF_REQUEST_TIMEOUT_SECONDS` | `10` | Per-request timeout (seconds) for Cloudflare API calls. |
| `LISTEN_ADDRESS` | `:9199` | Address and port the HTTP server listens on. |
| `METRICS_PATH` | `/metrics` | HTTP path where Prometheus metrics are served. |
| `LOG_LEVEL` | `info` | Log verbosity. One of `debug`, `info`, `warn`, `error`. |

## API Token Setup

The exporter requires a Cloudflare API **token** (not a Global API Key).

### Required Permissions

| Permission | Access | Needed For |
| --- | --- | --- |
| Account &rarr; Access: Apps and Policies | Read | Zero Trust Access login metrics |
| Account &rarr; Zero Trust | Read | Gateway DNS, network session, and browser isolation metrics |
| Account &rarr; Cloudflare Tunnel | Read | Tunnel status and request metrics |
| Zone &rarr; Analytics | Read | DNS analytics (GraphQL) |
| Zone &rarr; DNS | Read | DNS Firewall metrics |
| Zone &rarr; Zone | Read | Zone status, domain metadata |
| Zone &rarr; SSL and Certificates | Read | Certificate expiration monitoring |

### Creating a Token

1. Go to **My Profile > API Tokens** in the Cloudflare dashboard.
2. Click **Create Token**.
3. Select **Create Custom Token**.
4. Add the permissions listed above.
5. Under **Account Resources**, include the accounts you want to monitor
   (or "All accounts").
6. Under **Zone Resources**, include the zones you want to monitor (or
   "All zones").
7. Click **Continue to summary**, then **Create Token**.
8. Copy the token and set it as `CF_API_TOKEN`.

> **Tip:** Use the `--capabilities` CLI flag or the `/capabilities` HTTP
> endpoint to verify which collectors your token activates.

## Metrics Reference

### Zero Trust Metrics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `access_login_requests_total` | counter | `account`, `app_name`, `action` | Total Access login requests. |
| `gateway_dns_queries_total` | counter | `account`, `location`, `policy`, `category`, `action` | Total Gateway DNS queries. |
| `gateway_network_sessions_total` | counter | `account`, `policy`, `action` | Total Gateway network sessions. |
| `gateway_network_bytes_total` | counter | `account`, `policy`, `direction` | Total bytes through Gateway network sessions. |
| `browser_isolation_sessions_total` | counter | `account` | Total Browser Isolation sessions. |
| `tunnel_requests_total` | counter | `account`, `tunnel_name` | Total requests proxied through a Cloudflare Tunnel. |
| `tunnel_info` | gauge | `account`, `tunnel_name`, `tunnel_id`, `status` | Tunnel metadata; value is always 1. |

### DNS Metrics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `dns_queries_total` | counter | `zone`, `query_type`, `response_code` | Total authoritative DNS queries for a zone. |
| `dns_query_duration_seconds` | histogram | `zone` | DNS query processing time distribution. |
| `dns_firewall_queries_total` | counter | `zone`, `cluster`, `response_code` | Total queries handled by DNS Firewall. |

### Domain and Certificate Metrics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `domain_expiration_timestamp_seconds` | gauge | `zone`, `domain` | Unix timestamp when the domain registration expires. |
| `domain_auto_renew` | gauge | `zone`, `domain` | 1 if auto-renew is enabled, 0 otherwise. |
| `domain_locked` | gauge | `zone`, `domain` | 1 if the domain is locked, 0 otherwise. |
| `zone_status` | gauge | `zone`, `status` | 1 for the current zone status (active, pending, etc.). |
| `certificate_expiration_timestamp_seconds` | gauge | `zone`, `hostname`, `issuer` | Unix timestamp when the SSL certificate expires. |

### Self-Instrumentation Metrics

| Metric | Type | Description |
| --- | --- | --- |
| `cloudflare_exporter_scrape_duration_seconds` | histogram | Time spent collecting metrics from Cloudflare per cycle. |
| `cloudflare_exporter_scrape_errors_total` | counter | Total collection errors by collector name. |
| `cloudflare_exporter_graphql_requests_total` | counter | Total GraphQL API requests made. |
| `cloudflare_exporter_rest_requests_total` | counter | Total REST API requests made. |
| `cloudflare_exporter_graphql_budget_remaining` | gauge | Remaining GraphQL calls in the current budget window. |
| `cloudflare_exporter_rest_budget_remaining` | gauge | Remaining REST calls in the current budget window. |
| `cloudflare_exporter_discovery_runs_total` | counter | Total capability discovery runs. |
| `cloudflare_exporter_active_collectors` | gauge | Number of currently active collectors. |
| `cloudflare_exporter_build_info` | gauge | Build metadata (version, commit, Go version). Value is always 1. |

## Capability Discovery

The exporter probes the Cloudflare API on startup to determine which
products the account is entitled to and which permissions the token grants.
Only collectors whose prerequisites are met are activated.

### CLI Flag

```bash
docker run --rm \
  -e CF_API_TOKEN="your-token" \
  ghcr.io/phaseshiftdata/cloudflare_exporter:main \
  --capabilities
```

This prints a JSON summary of discovered capabilities and exits.

### HTTP Endpoint

While the exporter is running, `GET /capabilities` returns the same JSON
summary:

```bash
curl http://localhost:9199/capabilities
```

## Collection Model

### Windowing

Each collection cycle queries a fixed-width time window
(`CF_TIME_WINDOW_SECONDS`) that ends at `now - CF_SCRAPE_DELAY_SECONDS`.
The five-minute default delay accounts for Cloudflare's analytics pipeline
propagation latency; queries against more recent data risk returning
incomplete results.

### Propagation Delay

Cloudflare's GraphQL Analytics API has an inherent propagation delay of
roughly five minutes. The `CF_SCRAPE_DELAY_SECONDS` variable (default 300)
ensures the exporter only queries data that has fully landed.

### Counter Accumulation

Counters (such as `dns_queries_total`) are accumulated from the per-window
deltas returned by the API. On restart the counters reset to zero and
rebuild from the first successful collection. Prometheus `rate()` and
`increase()` handle this naturally.

### Deduplication

The aggregation store keys each sample by its full label set. If a
collection cycle returns a sample with the same labels as an existing entry,
the new value replaces the old one. This prevents double-counting when
windows overlap.

## Cardinality and Privacy

The exporter deliberately omits high-cardinality and privacy-sensitive
dimensions:

- **No per-IP breakdowns.** Source and destination IPs are never used as
  label values.
- **No query-name labels.** DNS query names (QNAMEs) are not exposed.
- **No per-URL path labels.** HTTP request paths are not included.

The `CF_GATEWAY_CATEGORY_TOP_N` setting caps the number of distinct
category label values per cycle, providing a hard cardinality ceiling on
the highest-cardinality metric family (`gateway_dns_queries_total`).

## Failure Modes

### Graceful Degradation

If a single collector fails (network timeout, permission revoked, API
error) the exporter logs the error, increments
`cloudflare_exporter_scrape_errors_total`, and continues serving metrics
from all other collectors. The failed collector retries on the next
scheduled cycle.

### Stale-but-Served

When a collector fails, the aggregation store continues to serve the
previous generation of that collector's metrics. Prometheus sees stale
but valid data rather than a scrape failure. The
`cloudflare_exporter_scrape_duration_seconds` histogram and error counters
allow you to alert on persistent staleness.

### Budget Exhaustion

If the quota governor's budget is exhausted, collectors are delayed rather
than dropped. Metrics remain at their last-known values until budget
refills. The `cloudflare_exporter_graphql_budget_remaining` and
`cloudflare_exporter_rest_budget_remaining` gauges enable proactive
alerting.

## Endpoints

| Path | Method | Description |
| --- | --- | --- |
| `/metrics` | GET | Prometheus metrics endpoint. |
| `/health` | GET | Health check. Returns `200 OK` when the exporter is running. |
| `/capabilities` | GET | JSON summary of discovered capabilities and active collectors. |

## Deployment

### Kubernetes

```yaml
apiVersion: apps/v1
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
            initialDelaySeconds: 10
            periodSeconds: 30
          readinessProbe:
            httpGet:
              path: /health
              port: 9199
            initialDelaySeconds: 5
            periodSeconds: 10
---
apiVersion: v1
kind: Service
metadata:
  name: cloudflare-exporter
spec:
  selector:
    app: cloudflare-exporter
  ports:
    - port: 9199
      targetPort: 9199
```

### Prometheus Scrape Config

```yaml
scrape_configs:
  - job_name: cloudflare
    scrape_interval: 60s
    static_configs:
      - targets:
          - cloudflare-exporter:9199
```

> **Note:** Align `scrape_interval` with `CF_REFRESH_INTERVAL_SECONDS` to
> avoid scraping between collection cycles.

### Single-Replica Topology

The exporter is designed to run as a **single replica**. Running multiple
replicas against the same API token multiplies API call volume and may
exhaust rate limits. If high availability is required, run one active
replica with a standby that only starts if the active fails.

## Free Plan Considerations

On Cloudflare's Free plan:

- **GraphQL Analytics may be unavailable.** The exporter falls back to
  REST-only collectors automatically via capability discovery.
- **Zero Trust metrics require a Zero Trust subscription.** Access,
  Gateway, Browser Isolation, and Tunnel collectors are disabled when the
  account lacks a Zero Trust plan.
- **Domain registration metrics** require domains registered through
  Cloudflare Registrar.
- **DNS Firewall metrics** require a DNS Firewall subscription.

The REST-only floor provides zone status, certificate expiration, and
domain metadata metrics on all plan levels, including Free.
