package db

import (
	"context"
	"fmt"
	"time"
)

// The reads in this file exist so that the exporter can answer one question
// without asking GitHub: has this already been collected?
//
// It could not answer that on 2026-08-11, and the consequence was a first poll
// that never finished. The collector fetched jobs once per workflow run, walked
// the whole of every repository's run history on every cycle, and re-asked for
// data it already had in PostgreSQL. Thousands of requests back to back tripped
// GitHub's secondary rate limit -- a 403 raised on request RATE, with the
// primary quota at 5247 of 5250 remaining. Quota was never the problem.
//
// So the store learned to be asked. Everything here is a read (or a small
// bookkeeping write) whose entire purpose is to remove requests that do not
// need to be sent.

// RunJobSync pairs a workflow run with the version of that run whose jobs are,
// or would be, on disk.
//
// SyncKey is the run's updated_at where GitHub supplied one and its created_at
// where it did not. A run whose updated_at has not moved cannot have gained,
// lost, or changed a job -- re-running the workflow, or a new attempt, moves it.
// The fallback to created_at is not cosmetic: updated_at is nullable, and NULL
// IS DISTINCT FROM NULL is false, so a run that arrived without one would
// compare equal to the never-fetched marker and would never have its jobs
// fetched at all. The coalescing is done in Go so that every SQL statement
// below can compare two plain timestamps.
type RunJobSync struct {
	RunID   int64
	SyncKey time.Time
}

// BackfillProgress is how far the paced historical walk has got for one
// repository. It lives in PostgreSQL because a restart must not start the walk
// -- and therefore the burst -- over again.
type BackfillProgress struct {
	Repo          string
	NextPage      int
	RunsComplete  bool
	CompletedAt   time.Time
	PagesFetched  int64
	RequestsSpent int64
}

// SelectRunsForJobFetch returns, out of the runs just observed from the API,
// the ones whose jobs actually have to be fetched.
//
// Callers upsert the runs first and then ask. Two kinds of run are deliberately
// left out of the answer:
//
//   - Runs already in sync. Their jobs endpoint has nothing new to say.
//
//   - Runs that are not in workflow_runs at all. That means the retention
//     prune consumed them between the upsert and this query -- they are older
//     than ninety days. UpsertWorkflowJob's WHERE EXISTS would discard their
//     jobs on arrival, so the request would buy a response that cannot be
//     stored. The join is inner for exactly that reason.
//
// The result keeps GitHub's newest-first order, so a caller that can only
// afford part of the list spends its budget on the most recent runs.
func (s *Store) SelectRunsForJobFetch(ctx context.Context, candidates []RunJobSync) ([]int64, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	ids := make([]int64, len(candidates))
	keys := make([]time.Time, len(candidates))
	for i, c := range candidates {
		ids[i] = c.RunID
		keys[i] = c.SyncKey
	}

	rows, err := s.pool.Query(ctx, selectRunsForJobFetchSQL, ids, keys)
	if err != nil {
		return nil, fmt.Errorf("selecting runs for job fetch: %w", err)
	}
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning run id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating runs for job fetch: %w", err)
	}
	return out, nil
}

// RunsMissingJobs finds stored runs for one repository whose jobs have never
// been fetched, or were fetched before the run last changed.
//
// This is the backfiller's work queue and it is derived, not stored. Nothing
// enqueues anything: the outstanding work is whatever the runs table says is
// outstanding, which is why a restart resumes rather than repeats, and why work
// deferred by the poll's per-cycle budget is never lost.
func (s *Store) RunsMissingJobs(ctx context.Context, repo string, limit int) ([]RunJobSync, error) {
	rows, err := s.pool.Query(ctx, runsMissingJobsSQL, repo, limit)
	if err != nil {
		return nil, fmt.Errorf("selecting runs missing jobs for %s: %w", repo, err)
	}
	defer rows.Close()

	var out []RunJobSync
	for rows.Next() {
		var r RunJobSync
		if err := rows.Scan(&r.RunID, &r.SyncKey); err != nil {
			return nil, fmt.Errorf("scanning run missing jobs: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating runs missing jobs: %w", err)
	}
	return out, nil
}

// BackfillStats is how much work is left, in one round trip.
type BackfillStats struct {
	// PendingJobRuns is how many stored runs are still waiting for a job fetch.
	PendingJobRuns int64
	// ReposComplete is how many repositories have walked their history to the
	// end of the horizon.
	ReposComplete int64
}

// BackfillStats returns how much backfill work remains.
//
// It exists to be published as gauges, and it is one statement rather than two
// because the backfiller asks on every tick. The stall on 2026-08-11 cost hours
// precisely because a collector with nothing to say looks exactly like a
// collector with nothing to do; a backlog that falls toward zero is the
// difference between "working through it" and "wedged".
func (s *Store) BackfillStats(ctx context.Context) (BackfillStats, error) {
	var st BackfillStats
	err := s.pool.QueryRow(ctx, backfillStatsSQL).Scan(&st.PendingJobRuns, &st.ReposComplete)
	if err != nil {
		return BackfillStats{}, fmt.Errorf("reading backfill stats: %w", err)
	}
	return st, nil
}

// MarkJobsSynced records that this run's jobs were fetched at this version of
// the run.
//
// It is written after the jobs are stored, never before. If the process dies
// between the two, the run is simply selected again next time -- one repeated
// request. Marking first and storing second would lose the jobs silently and
// permanently, which is the expensive direction to be wrong in.
func (s *Store) MarkJobsSynced(ctx context.Context, runID int64, syncKey time.Time) error {
	if _, err := s.pool.Exec(ctx, markJobsSyncedSQL, runID, syncKey); err != nil {
		return fmt.Errorf("marking jobs synced for run %d: %w", runID, err)
	}
	return nil
}

// ListRepositoryNames returns every known repository name, ordered, so the
// backfiller can rotate through them in a stable sequence.
//
// Archived repositories are included. They gain no new runs, but they can still
// hold history inside the retention window, and they cost one pass to finish
// and nothing thereafter.
func (s *Store) ListRepositoryNames(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, listRepositoryNamesSQL)
	if err != nil {
		return nil, fmt.Errorf("listing repository names: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning repository name: %w", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating repository names: %w", err)
	}
	return out, nil
}

// LoadBackfillProgress reads where the historical walk had got to for one
// repository. A repository nobody has walked yet reads as page 1, incomplete.
//
// The statement aggregates rather than selecting the row, so that a missing row
// returns a row of defaults instead of no rows at all. That keeps the caller
// free of a driver-specific "no rows" sentinel, and there is exactly one row
// per repository for MAX to pick from.
func (s *Store) LoadBackfillProgress(ctx context.Context, repo string) (BackfillProgress, error) {
	p := BackfillProgress{Repo: repo}
	err := s.pool.QueryRow(ctx, loadBackfillProgressSQL, repo).Scan(
		&p.NextPage, &p.RunsComplete, &p.CompletedAt, &p.PagesFetched, &p.RequestsSpent)
	if err != nil {
		return BackfillProgress{}, fmt.Errorf("loading backfill progress for %s: %w", repo, err)
	}
	return p, nil
}

// SaveBackfillProgress records where the walk has got to. It is called after
// every page, not at the end of a repository, because "the end" may be hours
// away and a restart in the meantime must not lose the pages already paid for.
func (s *Store) SaveBackfillProgress(ctx context.Context, p BackfillProgress) error {
	_, err := s.pool.Exec(ctx, saveBackfillProgressSQL,
		p.Repo, p.NextPage, p.RunsComplete, p.CompletedAt, p.PagesFetched, p.RequestsSpent)
	if err != nil {
		return fmt.Errorf("saving backfill progress for %s: %w", p.Repo, err)
	}
	return nil
}

// BackfillQueries exposes the statements above so that their shape can be
// asserted without a live database, matching UpsertQueries in store.go.
var BackfillQueries = map[string]string{
	"select_runs_for_job_fetch": selectRunsForJobFetchSQL,
	"runs_missing_jobs":         runsMissingJobsSQL,
	"backfill_stats":            backfillStatsSQL,
	"mark_jobs_synced":          markJobsSyncedSQL,
	"list_repository_names":     listRepositoryNamesSQL,
	"load_backfill_progress":    loadBackfillProgressSQL,
	"save_backfill_progress":    saveBackfillProgressSQL,
}

const selectRunsForJobFetchSQL = `
	SELECT wr.id
	FROM unnest($1::bigint[], $2::timestamptz[]) AS c(id, sync_key)
	JOIN workflow_runs wr ON wr.id = c.id
	WHERE wr.jobs_synced_at IS NULL
	   OR wr.jobs_synced_at IS DISTINCT FROM c.sync_key
	ORDER BY wr.created_at DESC`

const runsMissingJobsSQL = `
	SELECT id, COALESCE(updated_at, created_at)
	FROM workflow_runs
	WHERE repo = $1
	  AND (jobs_synced_at IS NULL
	       OR jobs_synced_at IS DISTINCT FROM COALESCE(updated_at, created_at))
	ORDER BY created_at DESC
	LIMIT $2`

const backfillStatsSQL = `
	SELECT
		(SELECT COUNT(*) FROM workflow_runs
		 WHERE jobs_synced_at IS NULL
		    OR jobs_synced_at IS DISTINCT FROM COALESCE(updated_at, created_at)),
		(SELECT COUNT(*) FROM backfill_progress WHERE runs_complete)`

const markJobsSyncedSQL = `
	UPDATE workflow_runs SET jobs_synced_at = $2 WHERE id = $1`

const listRepositoryNamesSQL = `
	SELECT name FROM repositories ORDER BY name`

const loadBackfillProgressSQL = `
	SELECT
		COALESCE(MAX(next_page), 1),
		COALESCE(BOOL_OR(runs_complete), FALSE),
		COALESCE(MAX(completed_at), to_timestamp(0)),
		COALESCE(MAX(pages_fetched), 0),
		COALESCE(MAX(requests_spent), 0)
	FROM backfill_progress
	WHERE repo = $1`

const saveBackfillProgressSQL = `
	INSERT INTO backfill_progress
		(repo, next_page, runs_complete, completed_at, pages_fetched, requests_spent, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, NOW())
	ON CONFLICT (repo) DO UPDATE SET
		next_page = EXCLUDED.next_page,
		runs_complete = EXCLUDED.runs_complete,
		completed_at = EXCLUDED.completed_at,
		pages_fetched = EXCLUDED.pages_fetched,
		requests_spent = EXCLUDED.requests_spent,
		updated_at = NOW()`
