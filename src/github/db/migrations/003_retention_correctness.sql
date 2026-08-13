-- 002 shipped the statement-level triggers issue #26 asks for, and they fire.
-- What they do not do is keep the data they claim to keep. Every problem below
-- was invisible until the migrations were applied to a real PostgreSQL and rows
-- were actually pushed through the ninetieth day, which is why the integration
-- tests that come with this migration exist.
--
--   1. The rollup overwrote instead of accumulating. Whenever one day's rows
--      arrive across two statements -- which is every day of the first backfill,
--      because the collectors page through history a hundred rows at a time --
--      the day ended up holding only the last statement's counts. Two runs on a
--      pruned day rolled up as one, and a success followed by a failure rolled
--      up as zero successes.
--
--   2. Pruning workflow_runs cascades into workflow_jobs, and nothing rolled
--      those jobs up before the cascade reached them. workflow_jobs_daily exists
--      so that slow-build attribution survives the prune; it was being emptied
--      by the prune instead.
--
--   3. workflow_jobs.started_at is the column the job prune filters on and it
--      carried no index, so every insert statement sequentially scanned the
--      table -- precisely the cost issue #26 says to design out.
--
-- The percentile columns 001 declared were never written by anything at all, so
-- the duration trends they exist for read as permanently empty. They are
-- computed here, from the timestamps, at rollup time. The raw timestamps stay
-- exactly as they are: durations are still derived, never stored in place of
-- the times they came from.
--
-- One writer per column, and this is the part worth reading twice. Every
-- cumulative column -- counts, percentiles -- is written only by the prune
-- triggers below, and only ever by adding, because a raw row passes through the
-- rollup exactly once: in the statement that deletes it. Nothing else may
-- recompute those columns from the raw tables. A second writer that recomputes
-- and overwrites cannot tell a day it has counted in full from a day whose
-- earlier rows have already been pruned, so it either doubles the count or
-- erases the history -- and it is silent either way. The only column the
-- triggers cannot produce is open_at_eod, which is a point-in-time sample
-- rather than a sum of events; the exporter writes that one, and nothing else.
--
-- The consequence for #25: these tables hold the compacted past, so a trend
-- spanning all history reads the rollups for days beyond ninety and the raw
-- tables for days inside it. That boundary is the one the dashboard is meant to
-- show rather than hide.
--
-- `tags` is deliberately EXEMPT from the ninety-day prune, and this note exists
-- so that nobody restores the symmetry.
--
-- Issue #26 lists tags among the raw events to prune, alongside runs, PRs and
-- commits. Those three are time series -- a stream of events, each meaningful on
-- the day it happened, and each summarised into a *_daily row before it goes.
-- Tags are not. The table is unique on (repo, name) and holds CURRENT STATE: the
-- releases a repository has, not a record of release events. There is no
-- tags_daily that would preserve them, so a prune is pure deletion.
--
-- It would also be silent and backwards. The dashboard's release panels read the
-- latest tag per repository and the time since it, so pruning at ninety days
-- empties them for exactly the repositories that have NOT shipped recently --
-- the ones the panel exists to surface. A repository that released last week
-- keeps its row; one that has not released in six months disappears entirely and
-- reads as though it has no releases at all.
--
-- Confirmed as intended 2026-08-10.
--
-- Note what follows from that: nothing removes a tag row automatically at all.
-- `tags.repo` is plain TEXT with no foreign key to `repositories`, so dropping a
-- repository from the org leaves its tags behind. At this volume -- a handful of
-- releases per repository -- that is cheaper than a retention rule that deletes
-- the history the panels read. If orphans ever need collecting, the correct
-- trigger is a repository disappearing, never the passage of time.

-- The pruned column, indexed. Without this the DELETE below is a sequential
-- scan on every insert statement, which is the one thing issue #26 calls out by
-- name as the reason to index.
CREATE INDEX IF NOT EXISTS idx_workflow_jobs_started ON workflow_jobs (started_at);

-- Merging two percentiles is an approximation and there is no way to make it
-- exact: the rows the earlier percentile was computed from have already been
-- deleted, which is the whole point of pruning. Weighting each side by its row
-- count is the standard compromise and it is only ever reached when a single
-- day's rows are pruned by more than one statement -- backfill paging, in
-- practice. Counts and maxima stay exact; only p50 and p95 blend.
CREATE OR REPLACE FUNCTION fn_blend_percentile(
    old_value NUMERIC, old_weight INTEGER,
    new_value NUMERIC, new_weight INTEGER
) RETURNS NUMERIC AS $$
    SELECT CASE
        WHEN new_value IS NULL THEN old_value
        WHEN old_value IS NULL THEN new_value
        ELSE (old_value * old_weight + new_value * new_weight)
             / NULLIF(old_weight + new_weight, 0)
    END;
$$ LANGUAGE sql IMMUTABLE;

-- Workflow runs: roll the children up, roll the runs up, then prune.
--
-- The job rollup has to come first and it has to be here rather than left to
-- the workflow_jobs trigger. DELETE FROM workflow_runs cascades, so the moment
-- the runs go the jobs go with them, and a trigger on workflow_jobs only fires
-- on insert -- it never sees a cascade. A job aggregated anywhere later is a job
-- aggregated after it stopped existing.
CREATE OR REPLACE FUNCTION fn_retain_workflow_runs() RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO workflow_jobs_daily (
        day, repo, workflow, job, runs, passed, failed,
        duration_p50_s, duration_p95_s, duration_max_s)
    SELECT
        DATE(wj.started_at),
        wr.repo,
        wr.workflow,
        wj.name,
        COUNT(*),
        COUNT(*) FILTER (WHERE wj.conclusion = 'success'),
        COUNT(*) FILTER (WHERE wj.conclusion = 'failure'),
        (PERCENTILE_CONT(0.5) WITHIN GROUP (
            ORDER BY EXTRACT(EPOCH FROM (wj.completed_at - wj.started_at))::float8))::NUMERIC,
        (PERCENTILE_CONT(0.95) WITHIN GROUP (
            ORDER BY EXTRACT(EPOCH FROM (wj.completed_at - wj.started_at))::float8))::NUMERIC,
        MAX(EXTRACT(EPOCH FROM (wj.completed_at - wj.started_at)))::NUMERIC
    FROM workflow_jobs wj
    JOIN workflow_runs wr ON wr.id = wj.run_id
    WHERE wr.created_at < NOW() - INTERVAL '90 days'
      AND wj.started_at IS NOT NULL
    GROUP BY DATE(wj.started_at), wr.repo, wr.workflow, wj.name
    ON CONFLICT (day, repo, workflow, job) DO UPDATE SET
        runs = workflow_jobs_daily.runs + EXCLUDED.runs,
        passed = workflow_jobs_daily.passed + EXCLUDED.passed,
        failed = workflow_jobs_daily.failed + EXCLUDED.failed,
        duration_p50_s = fn_blend_percentile(
            workflow_jobs_daily.duration_p50_s, workflow_jobs_daily.runs,
            EXCLUDED.duration_p50_s, EXCLUDED.runs),
        duration_p95_s = fn_blend_percentile(
            workflow_jobs_daily.duration_p95_s, workflow_jobs_daily.runs,
            EXCLUDED.duration_p95_s, EXCLUDED.runs),
        duration_max_s = GREATEST(workflow_jobs_daily.duration_max_s, EXCLUDED.duration_max_s);

    -- Queue time and execution time are kept apart because they have different
    -- causes and different fixes: queue is runners you do not have, execution is
    -- work the job is doing.
    INSERT INTO workflow_runs_daily (
        day, repo, workflow, runs, passed, failed, cancelled,
        queue_p50_s, queue_p95_s, exec_p50_s, exec_p95_s, exec_max_s)
    SELECT
        DATE(created_at),
        repo,
        workflow,
        COUNT(*),
        COUNT(*) FILTER (WHERE conclusion = 'success'),
        COUNT(*) FILTER (WHERE conclusion = 'failure'),
        COUNT(*) FILTER (WHERE conclusion = 'cancelled'),
        (PERCENTILE_CONT(0.5) WITHIN GROUP (
            ORDER BY EXTRACT(EPOCH FROM (run_started_at - created_at))::float8))::NUMERIC,
        (PERCENTILE_CONT(0.95) WITHIN GROUP (
            ORDER BY EXTRACT(EPOCH FROM (run_started_at - created_at))::float8))::NUMERIC,
        (PERCENTILE_CONT(0.5) WITHIN GROUP (
            ORDER BY EXTRACT(EPOCH FROM (updated_at - run_started_at))::float8))::NUMERIC,
        (PERCENTILE_CONT(0.95) WITHIN GROUP (
            ORDER BY EXTRACT(EPOCH FROM (updated_at - run_started_at))::float8))::NUMERIC,
        MAX(EXTRACT(EPOCH FROM (updated_at - run_started_at)))::NUMERIC
    FROM workflow_runs
    WHERE created_at < NOW() - INTERVAL '90 days'
    GROUP BY DATE(created_at), repo, workflow
    ON CONFLICT (day, repo, workflow) DO UPDATE SET
        runs = workflow_runs_daily.runs + EXCLUDED.runs,
        passed = workflow_runs_daily.passed + EXCLUDED.passed,
        failed = workflow_runs_daily.failed + EXCLUDED.failed,
        cancelled = workflow_runs_daily.cancelled + EXCLUDED.cancelled,
        queue_p50_s = fn_blend_percentile(
            workflow_runs_daily.queue_p50_s, workflow_runs_daily.runs,
            EXCLUDED.queue_p50_s, EXCLUDED.runs),
        queue_p95_s = fn_blend_percentile(
            workflow_runs_daily.queue_p95_s, workflow_runs_daily.runs,
            EXCLUDED.queue_p95_s, EXCLUDED.runs),
        exec_p50_s = fn_blend_percentile(
            workflow_runs_daily.exec_p50_s, workflow_runs_daily.runs,
            EXCLUDED.exec_p50_s, EXCLUDED.runs),
        exec_p95_s = fn_blend_percentile(
            workflow_runs_daily.exec_p95_s, workflow_runs_daily.runs,
            EXCLUDED.exec_p95_s, EXCLUDED.runs),
        exec_max_s = GREATEST(workflow_runs_daily.exec_max_s, EXCLUDED.exec_max_s);

    DELETE FROM workflow_runs
    WHERE created_at < NOW() - INTERVAL '90 days';

    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- Workflow jobs. This trigger still earns its place even though the run trigger
-- now handles the cascade: a job can age past ninety days while its run has not,
-- because a run is dated when it was created and a job when it started.
CREATE OR REPLACE FUNCTION fn_retain_workflow_jobs() RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO workflow_jobs_daily (
        day, repo, workflow, job, runs, passed, failed,
        duration_p50_s, duration_p95_s, duration_max_s)
    SELECT
        DATE(wj.started_at),
        wr.repo,
        wr.workflow,
        wj.name,
        COUNT(*),
        COUNT(*) FILTER (WHERE wj.conclusion = 'success'),
        COUNT(*) FILTER (WHERE wj.conclusion = 'failure'),
        (PERCENTILE_CONT(0.5) WITHIN GROUP (
            ORDER BY EXTRACT(EPOCH FROM (wj.completed_at - wj.started_at))::float8))::NUMERIC,
        (PERCENTILE_CONT(0.95) WITHIN GROUP (
            ORDER BY EXTRACT(EPOCH FROM (wj.completed_at - wj.started_at))::float8))::NUMERIC,
        MAX(EXTRACT(EPOCH FROM (wj.completed_at - wj.started_at)))::NUMERIC
    FROM workflow_jobs wj
    JOIN workflow_runs wr ON wr.id = wj.run_id
    WHERE wj.started_at < NOW() - INTERVAL '90 days'
    GROUP BY DATE(wj.started_at), wr.repo, wr.workflow, wj.name
    ON CONFLICT (day, repo, workflow, job) DO UPDATE SET
        runs = workflow_jobs_daily.runs + EXCLUDED.runs,
        passed = workflow_jobs_daily.passed + EXCLUDED.passed,
        failed = workflow_jobs_daily.failed + EXCLUDED.failed,
        duration_p50_s = fn_blend_percentile(
            workflow_jobs_daily.duration_p50_s, workflow_jobs_daily.runs,
            EXCLUDED.duration_p50_s, EXCLUDED.runs),
        duration_p95_s = fn_blend_percentile(
            workflow_jobs_daily.duration_p95_s, workflow_jobs_daily.runs,
            EXCLUDED.duration_p95_s, EXCLUDED.runs),
        duration_max_s = GREATEST(workflow_jobs_daily.duration_max_s, EXCLUDED.duration_max_s);

    DELETE FROM workflow_jobs
    WHERE started_at < NOW() - INTERVAL '90 days';

    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- Pull requests. Three separate aggregates because a pull request opened on
-- Monday and merged on Friday is one event on Monday and a different event on
-- Friday. 002 grouped all three by created_at, which reported every merge on
-- the day the branch was opened and made time-to-merge trends meaningless.
--
-- Open pull requests are deliberately never pruned, whatever their age. An open
-- pull request is not a historical event that has finished happening; it is
-- current state, and open_at_eod is sampled from these rows every day. Deleting
-- a pull request that is still open would silently drop it out of every future
-- sample, so the count would fall without anything having closed. Rows are
-- pruned once they have reached a terminal state and that state is ninety days
-- old. The visible effect is that `opened` for a past day keeps rising until
-- the last pull request opened that day finally closes, which is the honest
-- answer: until then the row is still readable in full detail from the raw
-- table, and counting it twice would be worse than counting it late.
CREATE OR REPLACE FUNCTION fn_retain_pull_requests() RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO pull_requests_daily (day, repo, opened)
    SELECT DATE(created_at), repo, COUNT(*)
    FROM pull_requests
    WHERE closed_at IS NOT NULL
      AND closed_at < NOW() - INTERVAL '90 days'
    GROUP BY DATE(created_at), repo
    ON CONFLICT (day, repo) DO UPDATE SET
        opened = pull_requests_daily.opened + EXCLUDED.opened;

    INSERT INTO pull_requests_daily (day, repo, merged, time_to_merge_p50_s)
    SELECT
        DATE(merged_at),
        repo,
        COUNT(*),
        (PERCENTILE_CONT(0.5) WITHIN GROUP (
            ORDER BY EXTRACT(EPOCH FROM (merged_at - created_at))::float8))::NUMERIC
    FROM pull_requests
    WHERE merged_at IS NOT NULL
      AND closed_at IS NOT NULL
      AND closed_at < NOW() - INTERVAL '90 days'
    GROUP BY DATE(merged_at), repo
    ON CONFLICT (day, repo) DO UPDATE SET
        merged = pull_requests_daily.merged + EXCLUDED.merged,
        time_to_merge_p50_s = fn_blend_percentile(
            pull_requests_daily.time_to_merge_p50_s, pull_requests_daily.merged,
            EXCLUDED.time_to_merge_p50_s, EXCLUDED.merged);

    INSERT INTO pull_requests_daily (day, repo, closed)
    SELECT DATE(closed_at), repo, COUNT(*)
    FROM pull_requests
    WHERE merged_at IS NULL
      AND closed_at IS NOT NULL
      AND closed_at < NOW() - INTERVAL '90 days'
    GROUP BY DATE(closed_at), repo
    ON CONFLICT (day, repo) DO UPDATE SET
        closed = pull_requests_daily.closed + EXCLUDED.closed;

    DELETE FROM pull_requests
    WHERE closed_at IS NOT NULL
      AND closed_at < NOW() - INTERVAL '90 days';

    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- The prune filters on closed_at now, so that is the column that needs the
-- index. created_at keeps its own from 001 because the detail panels in #25
-- order by it.
CREATE INDEX IF NOT EXISTS idx_pull_requests_closed ON pull_requests (closed_at);

-- Commits. Same accumulate-rather-than-overwrite fix; there are no percentiles
-- here because a commit is a point in time, not a duration.
CREATE OR REPLACE FUNCTION fn_retain_commits() RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO commits_daily (day, repo, branch, author, count)
    SELECT
        DATE(committed_at),
        repo,
        COALESCE(branch, ''),
        COALESCE(author, ''),
        COUNT(*)
    FROM commits
    WHERE committed_at < NOW() - INTERVAL '90 days'
    GROUP BY DATE(committed_at), repo, COALESCE(branch, ''), COALESCE(author, '')
    ON CONFLICT (day, repo, branch, author) DO UPDATE SET
        count = commits_daily.count + EXCLUDED.count;

    DELETE FROM commits
    WHERE committed_at < NOW() - INTERVAL '90 days';

    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
