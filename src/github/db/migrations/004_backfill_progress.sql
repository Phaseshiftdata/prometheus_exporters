-- This migration gives the exporter a memory of work it has already done. It
-- had none, and the cost of that was a cold start it could not survive.
--
-- 2026-08-11: the first poll collected 25 repositories and then stopped. Nine
-- minutes in, github_exporter_poll_duration_seconds_count was still 0 -- the
-- first poll had never finished. `up` was 1, /metrics answered 200, and nothing
-- was logged. The cause was a burst of API calls tripping GitHub's SECONDARY
-- rate limit, which answers 403; measured against the same App while it was
-- stuck, the PRIMARY quota was barely touched (limit 5250, used 3, remaining
-- 5247). It was never a shortage of quota. It was too many requests too close
-- together.
--
-- The burst comes from one request per workflow run for that run's jobs. One
-- repository here has 163 runs, run pagination had no page cap, and the whole
-- of history was re-walked from scratch on every single poll -- so every
-- fifteen minutes the exporter asked GitHub thousands of questions it already
-- had the answers to. PR #78 bounded the damage (the client gives up rather
-- than sleeping forever, and says so). This is the other half: not asking.
--
-- Two things have to be remembered for that, and they are remembered here
-- rather than in the exporter's memory because a restart must not begin again
-- from scratch. That is not hypothetical -- the exporter was restarted three
-- times while the stall was being diagnosed, and each restart threw away
-- whatever the previous one had managed to collect and started the same burst
-- over again.
--
--   1. workflow_runs.jobs_synced_at -- which runs have had their jobs fetched,
--      and at what version of the run. A run whose updated_at has not moved
--      cannot have gained, lost or changed a job, so its jobs endpoint has
--      nothing new to say and the request is pure waste. Storing the run's
--      updated_at (rather than a plain boolean) is what makes a re-run or a
--      re-attempt still get picked up: updated_at moves, the stored value no
--      longer matches, and the run is fetched again exactly once.
--
--      It is a column on workflow_runs and not a table of its own so that it
--      is deleted by the same cascade that deletes the run. A separate table
--      would need its own retention rule, and a retention rule that is ever
--      forgotten leaves rows claiming that runs which no longer exist are in
--      sync.
--
--   2. backfill_progress -- how far the slow historical walk has got, per
--      repository. Deep pagination is now a background activity that runs at a
--      bounded request rate and may take hours; it has to be able to stop in
--      the middle, and a process restart is the most ordinary way for that to
--      happen.
--
-- ETags are NOT a substitute for either of these, and it is worth being precise
-- about why, because it looks as though they should be. A 304 does not count
-- against the rate limit -- but it is still a request, and it is request COUNT
-- that trips the secondary limiter. Three thousand conditional requests that
-- all answer 304 cost no quota at all and will still get the exporter a 403.
-- The only request that is certainly safe is the one that is not sent.

-- A run is "in sync" when its jobs were fetched at its current updated_at.
-- NULL means never fetched, which is the correct initial state for every row
-- that already exists: nothing recorded a sync before this column existed, so
-- claiming otherwise would silently skip jobs the exporter has never seen.
ALTER TABLE workflow_runs ADD COLUMN IF NOT EXISTS jobs_synced_at TIMESTAMPTZ;

-- The index the backfiller reads. It is partial, on exactly the predicate the
-- backfiller asks, because the interesting set is the small one: in steady
-- state almost every run is in sync, and a full index would be a large
-- structure whose only use is finding the handful of rows that are not in it.
--
-- COALESCE(updated_at, created_at) rather than updated_at alone: updated_at is
-- nullable, and NULL IS DISTINCT FROM NULL is false, so a run that arrived
-- without an updated_at would compare equal to a never-fetched NULL marker and
-- would never be selected for a job fetch at all. created_at is NOT NULL and is
-- a fixed value for a given run, which is all the comparison needs.
CREATE INDEX IF NOT EXISTS idx_workflow_runs_jobs_pending
    ON workflow_runs (repo, created_at DESC)
    WHERE jobs_synced_at IS NULL
       OR jobs_synced_at IS DISTINCT FROM COALESCE(updated_at, created_at);

-- Where the historical walk has got to, per repository.
--
-- next_page is a page number and page numbers drift: GitHub returns runs newest
-- first, so a run created while the walk is in progress pushes everything one
-- place later and the walk can see a page twice or skip past one. That is
-- tolerated rather than engineered away, and it is only tolerable because of
-- the horizon: nothing is ever fetched from beyond the retention window, so
-- every row the walk can produce is a row the prune has NOT already consumed,
-- and re-inserting it is idempotent. Re-inserting a row from BEYOND the window
-- would not be idempotent at all -- migration 003's rollups accumulate, by
-- design, because a raw row passes through them exactly once in the statement
-- that deletes it. Offer the same pruned run twice and its day is counted
-- twice. Both boundaries are ninety days and the fetch horizon is deliberately
-- set inside that, so a fetched run cannot already be a counted run.
--
-- completed_at defaults to the epoch rather than NULL so that every read of
-- this table gets a comparable timestamp without a NULL check at each caller.
-- "Never completed" and "completed in 1970" mean the same thing here, and
-- runs_complete carries the real answer.
--
-- This table is deliberately EXEMPT from the ninety-day prune, in the same
-- spirit as `tags` in migration 003 and for the same reason: it is current
-- state, not a stream of events. There is no backfill_progress_daily that would
-- preserve it, and a prune would do nothing but make the exporter forget where
-- it was and start the burst this migration exists to prevent. There is no
-- foreign key to `repositories` either: a repository leaving the org should not
-- take a DELETE cascade through a table the backfiller reads on every tick, and
-- a stale row for a repository that no longer exists is simply never selected,
-- because the rotation is built from `repositories`.
CREATE TABLE IF NOT EXISTS backfill_progress (
    repo TEXT PRIMARY KEY,
    next_page INTEGER NOT NULL DEFAULT 1,
    runs_complete BOOLEAN NOT NULL DEFAULT FALSE,
    completed_at TIMESTAMPTZ NOT NULL DEFAULT to_timestamp(0),
    pages_fetched BIGINT NOT NULL DEFAULT 0,
    requests_spent BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
