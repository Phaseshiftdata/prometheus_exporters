# GitHub Exporter

## Overview

`github_exporter` is a Prometheus exporter that polls the GitHub API for
CI/CD, pull request, commit, and release data across all repositories in a
GitHub organization. It writes records to PostgreSQL and exposes aggregate
Prometheus metrics on a standard `/metrics` endpoint.

The exporter authenticates as a **GitHub App** (not a personal access token),
which gives it a dedicated 5,000 requests/hour rate limit independent of any
user's quota. Data is persisted in PostgreSQL so that dashboards can query
both real-time metrics and historical trends spanning months.

Two independent loops divide the work:

- **Poller** -- keeps recent data fresh. Runs every poll interval, fetches
  the first page of workflow runs per repository, and collects pull requests,
  commits, and tags. Finishes in seconds.
- **Backfiller** -- walks history at a bounded request rate. Issues at most
  one GitHub request every two seconds, may take hours to complete a first
  fill, and resumes from where it left off after a restart.

This separation exists because a single loop that tried to do both tripped
GitHub's secondary rate limit on day one and never finished its first poll.

## Architecture

### Poller

The poller runs on a configurable interval (default 5 minutes). Each cycle:

1. Fetches all repositories in the organization.
2. For each repository, fetches the first page of recent workflow runs
   (bounded to the collection horizon of 89 days).
3. Asks the database which of those runs need their jobs fetched (runs
   whose `updated_at` has not changed since the last fetch are skipped).
4. Fetches jobs only for changed runs, up to a per-cycle budget.
5. Collects pull requests, commits, and tags for each repository.
6. Samples the end-of-day open pull request count for today and yesterday.

The poller is explicitly the steady-state half of collection. It touches
one page of runs per repository and defers anything beyond its budget to
the backfiller.

### Backfiller

The backfiller runs continuously, issuing at most one GitHub request every
`--backfill-interval` (default 2 seconds). It:

1. Rotates through all known repositories in round-robin order so every
   dashboard fills in together rather than one repository monopolizing the
   request stream.
2. Prioritizes fetching jobs for runs that are missing them (filling holes
   in data the exporter already claims to have).
3. Pages deeper into each repository's run history when no jobs are pending.
4. Persists progress to PostgreSQL so that a restart resumes the walk
   instead of starting over.
5. Pauses automatically when the remaining primary rate limit falls below a
   configurable floor.

A strict minimum spacing between requests (rather than a token bucket) is
used because a bucket can accumulate permission and then spend it as a
burst, which is exactly the pattern that produced the original rate limit
incident.

### Rate Limit Management

GitHub enforces two distinct rate limits, and the exporter handles them
differently:

- **Primary limit** -- 5,000 requests per hour for GitHub App installations.
  The exporter tracks the remaining quota from response headers and the
  backfiller pauses when it falls below `--backfill-min-rate-limit`.
- **Secondary limit** -- burst and concurrency protection. Not about totals
  but about request rate. The exporter defends against this by limiting how
  many requests a single poll cycle can issue (`--job-budget-per-poll`) and
  by spacing backfill requests with a fixed interval.

The client classifies every 403 response as primary, secondary, or
unrelated, and reports the classification as a metric. Secondary limits
trigger a short backoff (typically 60 seconds); primary exhaustion waits
for the reset timestamp, capped at 2 minutes to avoid blocking a poll cycle.

### Pacing Controls

Three CLI flags control pacing, tunable based on how many repositories the
installation covers and how much of the App's quota the exporter should
spend on history:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--job-budget-per-poll` | 50 | Maximum job-fetch requests per poll cycle across all repos. |
| `--backfill-interval` | 2s | Minimum spacing between backfill requests. |
| `--backfill-min-rate-limit` | 500 | Pause backfill when remaining primary quota drops below this. |

### ETag Caching

The client caches ETags from GitHub responses. When the data has not
changed, GitHub returns 304 Not Modified, which costs no primary quota but
still counts as a request toward the secondary limit. ETags are used as an
optimization, not as a substitute for avoiding unnecessary requests.

### Collection Horizon

The exporter only fetches data within a 89-day collection horizon (one day
inside the 90-day retention window). This margin ensures that a fetched run
can never be one that the retention triggers have already rolled up and
deleted, which would cause double-counting in the daily rollup tables.

## Installation

Container images are published to GitHub Container Registry:

```
ghcr.io/phaseshiftdata/github_exporter
```

### Docker

```bash
docker run -d \
  --name github_exporter \
  -p 9102:9102 \
  -e DATABASE_URL="postgres://user:pass@db:5432/github_exporter" \
  -v /path/to/github-app-key.pem:/etc/github/key.pem:ro \
  ghcr.io/phaseshiftdata/github_exporter:main \
  --github-app-id 12345 \
  --github-install-id 67890 \
  --github-key-file /etc/github/key.pem \
  --org your-org \
  --listen-address 0.0.0.0:9102
```

### Docker Compose

```yaml
services:
  github_exporter:
    image: ghcr.io/phaseshiftdata/github_exporter:main
    ports:
      - "9102:9102"
    environment:
      DATABASE_URL: "postgres://github_exporter:${DB_PASSWORD}@postgres:5432/github_exporter"
    volumes:
      - ./github-app-key.pem:/etc/github/key.pem:ro
    command:
      - --github-app-id=12345
      - --github-install-id=67890
      - --github-key-file=/etc/github/key.pem
      - --org=your-org
      - --listen-address=0.0.0.0:9102
    restart: unless-stopped
    depends_on:
      - postgres

  postgres:
    image: postgres:16
    environment:
      POSTGRES_DB: github_exporter
      POSTGRES_USER: github_exporter
      POSTGRES_PASSWORD: "${DB_PASSWORD}"
    volumes:
      - pgdata:/var/lib/postgresql/data

volumes:
  pgdata:
```

## Configuration

All configuration is via CLI flags. The database URL can also be set via the
`DATABASE_URL` environment variable.

| Flag | Default | Description |
| --- | --- | --- |
| `--listen-address` | `127.0.0.1:9102` | Address to listen on for metrics. |
| `--database-url` | *(empty)* | PostgreSQL connection string (or `DATABASE_URL` env). |
| `--database-password-file` | *(empty)* | Path to file containing the database password (substituted into `--database-url`). |
| `--database-password-openbao` | *(empty)* | OpenBao KV path:field for the database password. |
| `--github-app-id` | `0` | GitHub App ID. |
| `--github-install-id` | `0` | GitHub App Installation ID. |
| `--github-key-file` | *(empty)* | Path to GitHub App private key PEM file. |
| `--poll-interval` | `5m` | Polling interval for fresh data. |
| `--org` | *(empty)* | GitHub organization to monitor. |
| `--log-level` | `info` | Log level (debug, info, warn, error). |
| `--job-budget-per-poll` | `50` | Maximum workflow-job requests one poll cycle may issue across all repos. |
| `--backfill-interval` | `2s` | Minimum spacing between historical backfill requests. |
| `--backfill-min-rate-limit` | `500` | Pause backfill while the remaining GitHub rate limit is below this. |
| `--openbao-address` | *(empty)* | OpenBao/Vault server address (env: `OPENBAO_ADDR`). |
| `--openbao-approle-role-id-file` | *(empty)* | Path to file containing the AppRole role_id. |
| `--openbao-approle-secret-id-file` | *(empty)* | Path to file containing the AppRole secret_id. |

### File-Based Secrets

The database password can be provided via a file instead of embedding it in
the connection string. The file is read at startup; trailing whitespace and
newlines are trimmed.

```bash
echo -n "db-password" > /run/secrets/db_password
github_exporter \
  --database-url "postgres://github_exporter@db:5432/github_exporter" \
  --database-password-file /run/secrets/db_password \
  --github-app-id 12345 \
  --github-install-id 67890 \
  --github-key-file /etc/github/key.pem \
  --org your-org
```

### OpenBao-Backed Secrets

For deployments using [OpenBao](https://openbao.org/) (or HashiCorp Vault)
as a central secret store, the database password can be read directly from
a KV v2 secret engine:

```bash
github_exporter \
  --database-url "postgres://github_exporter@db:5432/github_exporter" \
  --database-password-openbao secret/github/exporter:db_password \
  --openbao-address https://vault.example.com:8200 \
  --openbao-approle-role-id-file /run/secrets/role_id \
  --openbao-approle-secret-id-file /run/secrets/secret_id \
  --github-app-id 12345 \
  --github-install-id 67890 \
  --github-key-file /etc/github/key.pem \
  --org your-org
```

The reference format is `<kv-path>:<field>`. The exporter automatically
inserts the `/data/` segment required by the KV v2 API if it is missing.

### Secret Configuration Summary

The three sources for the database password are mutually exclusive. Setting
more than one source is a configuration error.

| Credential | Flag | File | OpenBao |
| --- | --- | --- | --- |
| Database password | Embedded in `--database-url` | `--database-password-file` | `--database-password-openbao` |

## GitHub App Setup

The exporter authenticates as a GitHub App installation, which provides a
dedicated rate limit of 5,000 requests per hour.

### Required Permissions

| Permission | Access | Needed For |
| --- | --- | --- |
| Repository &rarr; Actions | Read | Workflow runs and jobs |
| Repository &rarr; Contents | Read | Commits and tags |
| Repository &rarr; Metadata | Read | Repository discovery (granted by default) |
| Repository &rarr; Pull requests | Read | Pull request data |

### Creating the App

1. Go to your organization's **Settings > Developer settings > GitHub Apps**.
2. Click **New GitHub App**.
3. Fill in a name (e.g., "Prometheus GitHub Exporter").
4. Set the **Homepage URL** to your organization's URL.
5. Under **Webhook**, uncheck **Active** (the exporter polls; it does not
   use webhooks).
6. Under **Permissions**, grant the repository permissions listed above.
7. Under **Where can this GitHub App be installed?**, select
   **Only on this account**.
8. Click **Create GitHub App**.
9. Note the **App ID** shown on the app's settings page.

### Generating a Private Key

1. On the App's settings page, scroll to **Private keys**.
2. Click **Generate a private key**.
3. Save the downloaded PEM file securely.
4. Pass the path to the exporter via `--github-key-file`.

### Installing the App

1. On the App's settings page, click **Install App** in the sidebar.
2. Select the organization.
3. Choose **All repositories** (or select specific repositories).
4. Click **Install**.
5. Note the **Installation ID** from the URL (the number at the end of
   `/installations/<id>`).

## Database

### PostgreSQL Requirements

- PostgreSQL 14 or later.
- The exporter runs schema migrations automatically on startup.
- A dedicated database is recommended (e.g., `github_exporter`).

### Migrations

Four migrations are applied in order on first startup:

| Migration | Purpose |
| --- | --- |
| `001_initial.sql` | Creates raw event tables (repositories, workflow_runs, workflow_jobs, pull_requests, commits, tags) and daily rollup tables. |
| `002_retention_triggers.sql` | Adds statement-level triggers that roll up and prune raw data older than 90 days. |
| `003_retention_correctness.sql` | Fixes rollup accumulation (add instead of overwrite), adds percentile computation, ensures cascade-safe job rollup, and corrects pull request pruning. |
| `004_backfill_progress.sql` | Adds `jobs_synced_at` column to workflow_runs and a `backfill_progress` table for resumable historical walks. |

Migrations are forward-only and tracked in a `schema_migrations` table.

### Schema Overview

**Raw event tables** (pruned after 90 days, rolled up before deletion):

| Table | Primary Key | Description |
| --- | --- | --- |
| `repositories` | `id` | Organization repositories. |
| `workflow_runs` | `id` | GitHub Actions workflow runs. |
| `workflow_jobs` | `id` | Individual jobs within workflow runs (FK to workflow_runs with CASCADE). |
| `pull_requests` | `id` | Pull requests (open PRs are never pruned). |
| `commits` | `sha` | Commits on default branches. |
| `tags` | `id` | Git tags (exempt from pruning; current state, not events). |

**Daily rollup tables** (retained indefinitely):

| Table | Primary Key | Description |
| --- | --- | --- |
| `workflow_runs_daily` | `(day, repo, workflow)` | Daily run counts, pass/fail/cancel, queue and execution time percentiles. |
| `workflow_jobs_daily` | `(day, repo, workflow, job)` | Daily job counts, pass/fail, duration percentiles. |
| `pull_requests_daily` | `(day, repo)` | Daily opened/merged/closed counts, open-at-end-of-day, time-to-merge p50. |
| `commits_daily` | `(day, repo, branch, author)` | Daily commit counts by repo, branch, and author. |

**Operational tables:**

| Table | Primary Key | Description |
| --- | --- | --- |
| `backfill_progress` | `repo` | Per-repository backfill progress (page number, completion status, request counts). |
| `schema_migrations` | `name` | Applied migration tracking. |

### Retention

Raw event data is retained for 90 days. Statement-level triggers on INSERT
roll up expiring rows into the `*_daily` tables and then delete them. The
rollup uses accumulation (adding to existing counts) rather than
replacement, so rows arriving across multiple INSERT statements are counted
correctly.

Tags are exempt from the 90-day prune because they represent current state
(which releases a repository has), not a stream of events.

Open pull requests are never pruned regardless of age, because they are
current state sampled daily for the open-at-end-of-day metric.

## Metrics Reference

### Repository and CI/CD Metrics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `github_exporter_workflow_runs_total` | counter | `repo`, `workflow`, `conclusion` | Total workflow runs observed, by repo, workflow, and conclusion. |
| `github_exporter_open_pull_requests` | gauge | `repo` | Current number of open pull requests per repo. |
| `github_exporter_commits_total` | counter | `repo` | Total commits observed per repo. |

### Rate Limit Metrics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `github_exporter_rate_limit_remaining` | gauge | | GitHub API primary rate limit remaining. |
| `github_exporter_rate_limited_total` | counter | `kind` | 403 responses from GitHub, by which rate limit produced them (primary, secondary, none). |

### Poll Cycle Metrics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `github_exporter_poll_duration_seconds` | histogram | | Duration of a complete poll cycle in seconds. |
| `github_exporter_scrape_errors_total` | counter | `collector` | Total scrape errors by collector (repos, workflows, pullrequests, commits, tags, backfill). |
| `github_exporter_last_success_timestamp_seconds` | gauge | `collector` | Unix timestamp of last successful scrape by collector. |

### Request Accounting Metrics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `github_exporter_api_requests_total` | counter | `activity`, `kind` | GitHub API requests issued, by activity (poll, backfill) and kind (runs_page, jobs). |
| `github_exporter_job_fetches_skipped_total` | counter | | Workflow runs whose jobs were already stored and unchanged, so no request was made. |
| `github_exporter_job_budget_exhausted_total` | counter | | Poll cycles that hit their job-request budget and deferred the rest to the backfiller. |

### Backfill Metrics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `github_exporter_backfill_pending_job_runs` | gauge | | Stored workflow runs still waiting for their jobs to be fetched. |
| `github_exporter_backfill_repos_complete` | gauge | | Repositories whose historical run pagination has reached the end. |
| `github_exporter_backfill_repos_total` | gauge | | Repositories in the backfill rotation. |
| `github_exporter_backfill_throttled_total` | counter | `reason` | Backfill ticks that deliberately issued no request, by reason (rate_limit_headroom, no_work). |
| `github_exporter_backfill_paused` | gauge | | 1 while the backfiller is holding back to protect the rate limit, 0 otherwise. |
| `github_exporter_backfill_last_step_timestamp_seconds` | gauge | | Unix timestamp of the last backfill tick, whether or not it issued a request. |

## Rate Limiting

### Primary Rate Limit

GitHub App installations receive 5,000 requests per hour. The exporter
tracks the remaining quota from `X-RateLimit-Remaining` response headers.
When the remaining quota is low, the backfiller pauses automatically; the
poller continues because it issues a bounded number of requests per cycle.

A poll cycle with 25 repositories costs roughly `4 * repos + job_budget`
requests (about 150 with default settings), which fits comfortably within
the hourly quota.

### Secondary Rate Limit

GitHub's secondary rate limit protects against request bursts. It triggers
a 403 response regardless of how much primary quota remains. The exporter
defends against this by:

1. Fetching only one page of runs per repository per poll (not the full
   history).
2. Skipping job fetches for runs that have not changed (checked against the
   database).
3. Capping job fetches per cycle with `--job-budget-per-poll`.
4. Spacing backfill requests with a fixed interval rather than allowing
   bursts.

### Backfill Pacing

The backfiller issues at most one request every `--backfill-interval`
(default 2 seconds), giving roughly 1,800 requests per hour. Combined with
the poller's roughly 150 requests per cycle (every 5 minutes, so about
1,800/hour), the total stays well within the 5,000/hour primary quota and
the request rate stays below the secondary limit threshold.

A cold start with 25 repositories, each with a couple of hundred workflow
runs, takes a few hours to fully populate. This is the intended behavior:
an exporter that fills in visibly over hours is worth more than one that
completes in ten minutes and then wedges for the rest of the day.

## Failure Modes

### Rate Limiting

When the client receives a 403, it classifies the limit (primary vs.
secondary), logs which limit was hit, increments
`github_exporter_rate_limited_total`, and backs off:

- **Secondary limit:** Sleeps for the `Retry-After` header value, or 60
  seconds if no header is present. Retries up to 3 times.
- **Primary exhaustion:** Sleeps until the reset timestamp, capped at 2
  minutes. Returns an error after 3 retries so the poller can move on to
  the next repository.

A single failed jobs request stops that repository's job work for the
cycle. The backfiller picks it up later.

### Database Errors

If the database is unavailable, the exporter logs errors and increments
`github_exporter_scrape_errors_total`. It continues serving whatever
metrics it has. Individual collector failures do not halt the poll cycle;
each collector is independent.

### Authentication Failures

If the GitHub App private key is invalid or the installation token cannot
be obtained, the exporter exits with an error on startup. Token refresh
happens automatically; tokens are refreshed 5 minutes before expiry to
avoid races.

### No Database Configured

If no `--database-url` or `DATABASE_URL` is provided, the exporter runs in
metrics-only mode: it starts the HTTP server but does not poll GitHub or
write to a database. This is useful for verifying the deployment is
reachable.

## Endpoints

| Path | Method | Description |
| --- | --- | --- |
| `/metrics` | GET | Prometheus metrics endpoint. |
| `/` | GET | Landing page with a link to `/metrics`. |

## Deployment

### Kubernetes

```yaml
apiVersion: apps/v1
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
            initialDelaySeconds: 10
            periodSeconds: 30
          readinessProbe:
            httpGet:
              path: /
              port: 9102
            initialDelaySeconds: 5
            periodSeconds: 10
      volumes:
        - name: github-key
          secret:
            secretName: github-app-key
---
apiVersion: v1
kind: Service
metadata:
  name: github-exporter
spec:
  selector:
    app: github-exporter
  ports:
    - port: 9102
      targetPort: 9102
```

### Prometheus Scrape Config

```yaml
scrape_configs:
  - job_name: github
    scrape_interval: 60s
    static_configs:
      - targets:
          - github-exporter:9102
```

> **Note:** The scrape interval can be shorter than the poll interval.
> Between polls, Prometheus receives the same metric values. The poll
> interval controls how quickly new data appears, not how often Prometheus
> can read it.

### Single-Replica Topology

The exporter is designed to run as a **single replica**. Running multiple
replicas against the same GitHub App installation multiplies API call
volume and may exhaust the shared rate limit. Both replicas would also
write to the same database, causing duplicate data in the daily rollups.
If high availability is required, run one active replica with a standby
that only starts if the active fails.
