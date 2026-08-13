-- Raw event tables
CREATE TABLE IF NOT EXISTS repositories (
    id BIGINT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    default_branch TEXT,
    visibility TEXT,
    archived BOOLEAN DEFAULT FALSE,
    updated_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS workflow_runs (
    id BIGINT PRIMARY KEY,
    repo TEXT NOT NULL,
    workflow TEXT NOT NULL,
    branch TEXT,
    conclusion TEXT,
    attempt INTEGER DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL,
    run_started_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_created ON workflow_runs (created_at);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_repo ON workflow_runs (repo, workflow);

CREATE TABLE IF NOT EXISTS workflow_jobs (
    id BIGINT PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    conclusion TEXT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_workflow_jobs_run ON workflow_jobs (run_id);

CREATE TABLE IF NOT EXISTS pull_requests (
    id BIGINT PRIMARY KEY,
    repo TEXT NOT NULL,
    number INTEGER NOT NULL,
    state TEXT NOT NULL,
    author TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    merged_at TIMESTAMPTZ,
    closed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_pull_requests_created ON pull_requests (created_at);
CREATE INDEX IF NOT EXISTS idx_pull_requests_repo ON pull_requests (repo);

CREATE TABLE IF NOT EXISTS commits (
    sha TEXT PRIMARY KEY,
    repo TEXT NOT NULL,
    branch TEXT,
    author TEXT,
    message TEXT,
    committed_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_commits_committed ON commits (committed_at);
CREATE INDEX IF NOT EXISTS idx_commits_repo ON commits (repo);

CREATE TABLE IF NOT EXISTS tags (
    id SERIAL PRIMARY KEY,
    repo TEXT NOT NULL,
    name TEXT NOT NULL,
    target_sha TEXT,
    created_at TIMESTAMPTZ,
    UNIQUE(repo, name)
);

-- Daily rollup tables (retained indefinitely)
CREATE TABLE IF NOT EXISTS workflow_runs_daily (
    day DATE NOT NULL,
    repo TEXT NOT NULL,
    workflow TEXT NOT NULL,
    runs INTEGER NOT NULL DEFAULT 0,
    passed INTEGER NOT NULL DEFAULT 0,
    failed INTEGER NOT NULL DEFAULT 0,
    cancelled INTEGER NOT NULL DEFAULT 0,
    queue_p50_s NUMERIC,
    queue_p95_s NUMERIC,
    exec_p50_s NUMERIC,
    exec_p95_s NUMERIC,
    exec_max_s NUMERIC,
    PRIMARY KEY (day, repo, workflow)
);

CREATE TABLE IF NOT EXISTS workflow_jobs_daily (
    day DATE NOT NULL,
    repo TEXT NOT NULL,
    workflow TEXT NOT NULL,
    job TEXT NOT NULL,
    runs INTEGER NOT NULL DEFAULT 0,
    passed INTEGER NOT NULL DEFAULT 0,
    failed INTEGER NOT NULL DEFAULT 0,
    duration_p50_s NUMERIC,
    duration_p95_s NUMERIC,
    duration_max_s NUMERIC,
    PRIMARY KEY (day, repo, workflow, job)
);

CREATE TABLE IF NOT EXISTS commits_daily (
    day DATE NOT NULL,
    repo TEXT NOT NULL,
    branch TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (day, repo, branch, author)
);

CREATE TABLE IF NOT EXISTS pull_requests_daily (
    day DATE NOT NULL,
    repo TEXT NOT NULL,
    opened INTEGER NOT NULL DEFAULT 0,
    merged INTEGER NOT NULL DEFAULT 0,
    closed INTEGER NOT NULL DEFAULT 0,
    open_at_eod INTEGER NOT NULL DEFAULT 0,
    time_to_merge_p50_s NUMERIC,
    PRIMARY KEY (day, repo)
);
