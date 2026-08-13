-- Retention triggers: prune rows older than 90 days after insert.
-- Before pruning, ensure daily rollups exist for the days being removed.
-- This implements the rollup-before-prune pattern from issue #26:
-- 1. Compute rollups for any day that has raw data older than 90 days
-- 2. Delete the raw rows older than 90 days

-- Workflow runs: rollup then prune
CREATE OR REPLACE FUNCTION fn_retain_workflow_runs() RETURNS TRIGGER AS $$
BEGIN
    -- Rollup any days about to lose data
    INSERT INTO workflow_runs_daily (day, repo, workflow, runs, passed, failed, cancelled)
    SELECT
        DATE(created_at) AS day,
        repo,
        workflow,
        COUNT(*) AS runs,
        COUNT(*) FILTER (WHERE conclusion = 'success') AS passed,
        COUNT(*) FILTER (WHERE conclusion = 'failure') AS failed,
        COUNT(*) FILTER (WHERE conclusion = 'cancelled') AS cancelled
    FROM workflow_runs
    WHERE created_at < NOW() - INTERVAL '90 days'
    GROUP BY DATE(created_at), repo, workflow
    ON CONFLICT (day, repo, workflow) DO UPDATE SET
        runs = EXCLUDED.runs,
        passed = EXCLUDED.passed,
        failed = EXCLUDED.failed,
        cancelled = EXCLUDED.cancelled;

    -- Prune old rows
    DELETE FROM workflow_runs
    WHERE created_at < NOW() - INTERVAL '90 days';

    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_retain_workflow_runs ON workflow_runs;
CREATE TRIGGER trg_retain_workflow_runs
    AFTER INSERT ON workflow_runs
    FOR EACH STATEMENT
    EXECUTE FUNCTION fn_retain_workflow_runs();

-- Workflow jobs: rollup then prune
CREATE OR REPLACE FUNCTION fn_retain_workflow_jobs() RETURNS TRIGGER AS $$
BEGIN
    -- Rollup any days about to lose data
    INSERT INTO workflow_jobs_daily (day, repo, workflow, job, runs, passed, failed)
    SELECT
        DATE(wj.started_at) AS day,
        wr.repo,
        wr.workflow,
        wj.name AS job,
        COUNT(*) AS runs,
        COUNT(*) FILTER (WHERE wj.conclusion = 'success') AS passed,
        COUNT(*) FILTER (WHERE wj.conclusion = 'failure') AS failed
    FROM workflow_jobs wj
    JOIN workflow_runs wr ON wr.id = wj.run_id
    WHERE wj.started_at < NOW() - INTERVAL '90 days'
    GROUP BY DATE(wj.started_at), wr.repo, wr.workflow, wj.name
    ON CONFLICT (day, repo, workflow, job) DO UPDATE SET
        runs = EXCLUDED.runs,
        passed = EXCLUDED.passed,
        failed = EXCLUDED.failed;

    -- Prune old rows (cascade from workflow_runs handles jobs whose runs are pruned,
    -- but jobs may also be old independently)
    DELETE FROM workflow_jobs
    WHERE started_at < NOW() - INTERVAL '90 days';

    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_retain_workflow_jobs ON workflow_jobs;
CREATE TRIGGER trg_retain_workflow_jobs
    AFTER INSERT ON workflow_jobs
    FOR EACH STATEMENT
    EXECUTE FUNCTION fn_retain_workflow_jobs();

-- Pull requests: rollup then prune
CREATE OR REPLACE FUNCTION fn_retain_pull_requests() RETURNS TRIGGER AS $$
BEGIN
    -- Rollup any days about to lose data
    INSERT INTO pull_requests_daily (day, repo, opened, merged, closed)
    SELECT
        DATE(created_at) AS day,
        repo,
        COUNT(*) FILTER (WHERE state IN ('open', 'closed')) AS opened,
        COUNT(*) FILTER (WHERE merged_at IS NOT NULL) AS merged,
        COUNT(*) FILTER (WHERE state = 'closed' AND merged_at IS NULL) AS closed
    FROM pull_requests
    WHERE created_at < NOW() - INTERVAL '90 days'
    GROUP BY DATE(created_at), repo
    ON CONFLICT (day, repo) DO UPDATE SET
        opened = EXCLUDED.opened,
        merged = EXCLUDED.merged,
        closed = EXCLUDED.closed;

    -- Prune old rows
    DELETE FROM pull_requests
    WHERE created_at < NOW() - INTERVAL '90 days';

    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_retain_pull_requests ON pull_requests;
CREATE TRIGGER trg_retain_pull_requests
    AFTER INSERT ON pull_requests
    FOR EACH STATEMENT
    EXECUTE FUNCTION fn_retain_pull_requests();

-- Commits: rollup then prune
CREATE OR REPLACE FUNCTION fn_retain_commits() RETURNS TRIGGER AS $$
BEGIN
    -- Rollup any days about to lose data
    INSERT INTO commits_daily (day, repo, branch, author, count)
    SELECT
        DATE(committed_at) AS day,
        repo,
        COALESCE(branch, '') AS branch,
        COALESCE(author, '') AS author,
        COUNT(*) AS count
    FROM commits
    WHERE committed_at < NOW() - INTERVAL '90 days'
    GROUP BY DATE(committed_at), repo, COALESCE(branch, ''), COALESCE(author, '')
    ON CONFLICT (day, repo, branch, author) DO UPDATE SET
        count = EXCLUDED.count;

    -- Prune old rows
    DELETE FROM commits
    WHERE committed_at < NOW() - INTERVAL '90 days';

    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_retain_commits ON commits;
CREATE TRIGGER trg_retain_commits
    AFTER INSERT ON commits
    FOR EACH STATEMENT
    EXECUTE FUNCTION fn_retain_commits();
