package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// ---- statement shape, without a database

func TestBackfillQueries_AreExposedAndNonEmpty(t *testing.T) {
	want := []string{
		"select_runs_for_job_fetch", "runs_missing_jobs", "backfill_stats",
		"mark_jobs_synced", "list_repository_names",
		"load_backfill_progress", "save_backfill_progress",
	}
	for _, name := range want {
		sql, ok := BackfillQueries[name]
		if !ok {
			t.Errorf("BackfillQueries is missing %q", name)
			continue
		}
		if strings.TrimSpace(sql) == "" {
			t.Errorf("BackfillQueries[%q] is empty", name)
		}
	}
}

// The nullable-updated_at trap, asserted on the statement itself: comparing a
// NULL jobs_synced_at with a NULL updated_at would be "not distinct", and the
// run would never be selected for a job fetch at all.
func TestBackfillQueries_CoalesceTheNullableUpdatedAt(t *testing.T) {
	for _, name := range []string{"runs_missing_jobs", "backfill_stats"} {
		if !strings.Contains(BackfillQueries[name], "COALESCE(updated_at, created_at)") {
			t.Errorf("%s does not coalesce the nullable updated_at:\n%s", name, BackfillQueries[name])
		}
	}
}

// The join in select_runs_for_job_fetch is inner on purpose: a run that is not
// stored has been pruned, and its jobs would be discarded on arrival by
// UpsertWorkflowJob's WHERE EXISTS. Asking for them spends a request on a
// response that cannot be kept.
func TestSelectRunsForJobFetch_DoesNotOuterJoin(t *testing.T) {
	sql := BackfillQueries["select_runs_for_job_fetch"]
	if strings.Contains(strings.ToUpper(sql), "LEFT JOIN") {
		t.Errorf("statement outer-joins, so pruned runs would be selected:\n%s", sql)
	}
	if !strings.Contains(sql, "unnest($1::bigint[], $2::timestamptz[])") {
		t.Errorf("statement does not take the candidates as arrays:\n%s", sql)
	}
}

func TestSelectRunsForJobFetch_EmptyCandidatesAsksNothing(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)

	got, err := store.SelectRunsForJobFetch(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(pool.getExecCalls()) != 0 {
		t.Error("an empty candidate list should not reach the database")
	}
}

// ---- failure paths

type failingPool struct {
	queryErr    error
	queryRowErr error
	execErr     error
	rows        *erroringRows
}

func (p *failingPool) Exec(_ context.Context, _ string, _ ...any) (CommandTag, error) {
	if p.execErr != nil {
		return nil, p.execErr
	}
	return mockCommandTag{rowsAffected: 1}, nil
}

func (p *failingPool) Query(_ context.Context, _ string, _ ...any) (Rows, error) {
	if p.queryErr != nil {
		return nil, p.queryErr
	}
	return p.rows, nil
}

func (p *failingPool) QueryRow(_ context.Context, _ string, _ ...any) Row {
	return &mockRow{err: p.queryRowErr}
}

// erroringRows yields one row that fails to scan, or reports an iteration
// error, depending on how it is configured.
type erroringRows struct {
	remaining int
	scanErr   error
	iterErr   error
}

func (r *erroringRows) Next() bool {
	if r.remaining > 0 {
		r.remaining--
		return true
	}
	return false
}
func (r *erroringRows) Scan(_ ...any) error { return r.scanErr }
func (r *erroringRows) Err() error          { return r.iterErr }
func (r *erroringRows) Close()              {}

func TestBackfillReads_ReportFailuresRatherThanReturningEmpty(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("connection reset")
	candidates := []RunJobSync{{RunID: 1, SyncKey: time.Now()}}

	cases := []struct {
		name string
		pool DBPool
		call func(*Store) error
	}{
		{
			name: "select query fails",
			pool: &failingPool{queryErr: boom},
			call: func(s *Store) error {
				_, err := s.SelectRunsForJobFetch(ctx, candidates)
				return err
			},
		},
		{
			name: "select scan fails",
			pool: &failingPool{rows: &erroringRows{remaining: 1, scanErr: boom}},
			call: func(s *Store) error {
				_, err := s.SelectRunsForJobFetch(ctx, candidates)
				return err
			},
		},
		{
			name: "select iteration fails",
			pool: &failingPool{rows: &erroringRows{iterErr: boom}},
			call: func(s *Store) error {
				_, err := s.SelectRunsForJobFetch(ctx, candidates)
				return err
			},
		},
		{
			name: "pending query fails",
			pool: &failingPool{queryErr: boom},
			call: func(s *Store) error {
				_, err := s.RunsMissingJobs(ctx, "repo", 1)
				return err
			},
		},
		{
			name: "pending scan fails",
			pool: &failingPool{rows: &erroringRows{remaining: 1, scanErr: boom}},
			call: func(s *Store) error {
				_, err := s.RunsMissingJobs(ctx, "repo", 1)
				return err
			},
		},
		{
			name: "pending iteration fails",
			pool: &failingPool{rows: &erroringRows{iterErr: boom}},
			call: func(s *Store) error {
				_, err := s.RunsMissingJobs(ctx, "repo", 1)
				return err
			},
		},
		{
			name: "repository listing fails",
			pool: &failingPool{queryErr: boom},
			call: func(s *Store) error {
				_, err := s.ListRepositoryNames(ctx)
				return err
			},
		},
		{
			name: "repository scan fails",
			pool: &failingPool{rows: &erroringRows{remaining: 1, scanErr: boom}},
			call: func(s *Store) error {
				_, err := s.ListRepositoryNames(ctx)
				return err
			},
		},
		{
			name: "repository iteration fails",
			pool: &failingPool{rows: &erroringRows{iterErr: boom}},
			call: func(s *Store) error {
				_, err := s.ListRepositoryNames(ctx)
				return err
			},
		},
		{
			name: "stats fails",
			pool: &failingPool{queryRowErr: boom},
			call: func(s *Store) error {
				_, err := s.BackfillStats(ctx)
				return err
			},
		},
		{
			name: "progress load fails",
			pool: &failingPool{queryRowErr: boom},
			call: func(s *Store) error {
				_, err := s.LoadBackfillProgress(ctx, "repo")
				return err
			},
		},
		{
			name: "progress save fails",
			pool: &failingPool{execErr: boom},
			call: func(s *Store) error {
				return s.SaveBackfillProgress(ctx, BackfillProgress{Repo: "repo"})
			},
		},
		{
			name: "marking synced fails",
			pool: &failingPool{execErr: boom},
			call: func(s *Store) error {
				return s.MarkJobsSynced(ctx, 1, time.Now())
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(NewStore(tc.pool)); err == nil {
				t.Fatal("expected an error to be reported")
			}
		})
	}
}

// ---- against a live PostgreSQL

func insertRun(t *testing.T, store *Store, ctx context.Context, id int64, repo string, created time.Time, updated *time.Time) {
	t.Helper()
	if err := store.UpsertWorkflowRun(ctx, WorkflowRun{
		ID: id, Repo: repo, Workflow: "CI", Branch: "main", Conclusion: "success",
		Attempt: 1, CreatedAt: created, RunStartedAt: &created, UpdatedAt: updated,
	}); err != nil {
		t.Fatalf("inserting run %d: %v", id, err)
	}
}

// The decision the whole cold-start fix turns on: which runs need a jobs
// request. Everything else is pacing.
func TestIntegration_SelectRunsForJobFetch(t *testing.T) {
	store, pool, ctx := migratedStore(t)

	now := time.Now().UTC()
	updated := now.Add(-time.Hour)
	insertRun(t, store, ctx, 1, "repo-a", now.Add(-2*time.Hour), &updated) // never synced
	insertRun(t, store, ctx, 2, "repo-a", now.Add(-2*time.Hour), &updated) // synced, unchanged
	insertRun(t, store, ctx, 3, "repo-a", now.Add(-2*time.Hour), &updated) // synced, then changed

	if err := store.MarkJobsSynced(ctx, 2, updated); err != nil {
		t.Fatalf("marking run 2: %v", err)
	}
	if err := store.MarkJobsSynced(ctx, 3, updated); err != nil {
		t.Fatalf("marking run 3: %v", err)
	}
	// Run 3 has since been re-run: updated_at moves, so its jobs are stale.
	changed := now.Add(-time.Minute)
	insertRun(t, store, ctx, 3, "repo-a", now.Add(-2*time.Hour), &changed)

	got, err := store.SelectRunsForJobFetch(ctx, []RunJobSync{
		{RunID: 1, SyncKey: updated},
		{RunID: 2, SyncKey: updated},
		{RunID: 3, SyncKey: changed},
		{RunID: 99, SyncKey: updated}, // never stored: pruned, or too old to keep
	})
	if err != nil {
		t.Fatalf("SelectRunsForJobFetch: %v", err)
	}

	want := map[int64]bool{1: true, 3: true}
	if len(got) != len(want) {
		t.Fatalf("selected %v, want runs 1 and 3", got)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("run %d should not have been selected", id)
		}
	}

	// And the saving is real: exactly one run is in sync, so exactly one job
	// request is not made. Note that run 3 still CARRIES a jobs_synced_at --
	// the upsert does not clear it -- which is why "in sync" has to be a
	// comparison against the run's current version rather than a null check.
	inSync := queryInt(t, ctx, pool, `
		SELECT COUNT(*) FROM workflow_runs
		 WHERE jobs_synced_at IS NOT NULL
		   AND jobs_synced_at = COALESCE(updated_at, created_at)`)
	if inSync != 1 {
		t.Errorf("%d runs count as in sync, want 1", inSync)
	}
}

// A run with no updated_at at all must still be fetchable. Comparing NULL with
// NULL is "not distinct", so without the created_at fallback this run would sit
// in the table forever, never selected and never fetched.
func TestIntegration_RunWithoutUpdatedAtIsStillFetchedOnce(t *testing.T) {
	store, _, ctx := migratedStore(t)

	created := time.Now().UTC().Add(-time.Hour)
	insertRun(t, store, ctx, 1, "repo-a", created, nil)

	pending, err := store.RunsMissingJobs(ctx, "repo-a", 10)
	if err != nil {
		t.Fatalf("RunsMissingJobs: %v", err)
	}
	if len(pending) != 1 || pending[0].RunID != 1 {
		t.Fatalf("expected run 1 to be pending, got %+v", pending)
	}
	if !pending[0].SyncKey.UTC().Truncate(time.Second).Equal(created.Truncate(time.Second)) {
		t.Errorf("sync key = %v, want the created_at fallback %v", pending[0].SyncKey, created)
	}

	if err := store.MarkJobsSynced(ctx, 1, pending[0].SyncKey); err != nil {
		t.Fatalf("MarkJobsSynced: %v", err)
	}
	pending, err = store.RunsMissingJobs(ctx, "repo-a", 10)
	if err != nil {
		t.Fatalf("RunsMissingJobs: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("run is still pending after being marked: %+v", pending)
	}
}

func TestIntegration_RunsMissingJobsIsPerRepoNewestFirstAndLimited(t *testing.T) {
	store, _, ctx := migratedStore(t)

	now := time.Now().UTC()
	for i := range 5 {
		insertRun(t, store, ctx, int64(100+i), "repo-a", now.Add(-time.Duration(i)*time.Hour), nil)
	}
	insertRun(t, store, ctx, 200, "repo-b", now, nil)

	pending, err := store.RunsMissingJobs(ctx, "repo-a", 2)
	if err != nil {
		t.Fatalf("RunsMissingJobs: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected the limit to be honored, got %d", len(pending))
	}
	if pending[0].RunID != 100 || pending[1].RunID != 101 {
		t.Errorf("expected newest first (100, 101), got %d, %d", pending[0].RunID, pending[1].RunID)
	}
}

func TestIntegration_BackfillStatsCountWhatIsLeft(t *testing.T) {
	store, _, ctx := migratedStore(t)

	now := time.Now().UTC()
	insertRun(t, store, ctx, 1, "repo-a", now, nil)
	insertRun(t, store, ctx, 2, "repo-a", now, nil)
	if err := store.MarkJobsSynced(ctx, 1, now); err != nil {
		t.Fatalf("MarkJobsSynced: %v", err)
	}

	if err := store.SaveBackfillProgress(ctx, BackfillProgress{
		Repo: "repo-a", NextPage: 1, RunsComplete: true, CompletedAt: now,
	}); err != nil {
		t.Fatalf("SaveBackfillProgress: %v", err)
	}
	if err := store.SaveBackfillProgress(ctx, BackfillProgress{
		Repo: "repo-b", NextPage: 3,
	}); err != nil {
		t.Fatalf("SaveBackfillProgress: %v", err)
	}

	stats, err := store.BackfillStats(ctx)
	if err != nil {
		t.Fatalf("BackfillStats: %v", err)
	}
	if stats.PendingJobRuns != 1 {
		t.Errorf("PendingJobRuns = %d, want 1", stats.PendingJobRuns)
	}
	if stats.ReposComplete != 1 {
		t.Errorf("ReposComplete = %d, want 1", stats.ReposComplete)
	}
}

// Progress has to survive a restart, which here means: it survives being read
// back by a store that has never seen it before.
func TestIntegration_BackfillProgressRoundTrip(t *testing.T) {
	store, _, ctx := migratedStore(t)

	// A repository nobody has walked reads as page 1, incomplete -- not as an
	// error and not as "no rows".
	fresh, err := store.LoadBackfillProgress(ctx, "never-seen")
	if err != nil {
		t.Fatalf("LoadBackfillProgress: %v", err)
	}
	if fresh.NextPage != 1 || fresh.RunsComplete {
		t.Errorf("unwalked repository reads as %+v, want page 1 and incomplete", fresh)
	}

	completed := time.Now().UTC().Truncate(time.Second)
	want := BackfillProgress{
		Repo: "repo-a", NextPage: 7, RunsComplete: true,
		CompletedAt: completed, PagesFetched: 6, RequestsSpent: 9,
	}
	if err := store.SaveBackfillProgress(ctx, want); err != nil {
		t.Fatalf("SaveBackfillProgress: %v", err)
	}

	got, err := store.LoadBackfillProgress(ctx, "repo-a")
	if err != nil {
		t.Fatalf("LoadBackfillProgress: %v", err)
	}
	if got.NextPage != want.NextPage || got.RunsComplete != want.RunsComplete ||
		got.PagesFetched != want.PagesFetched || got.RequestsSpent != want.RequestsSpent ||
		!got.CompletedAt.UTC().Equal(completed) {
		t.Errorf("progress round-tripped as %+v, want %+v", got, want)
	}

	// Saving again updates in place rather than failing on the primary key.
	want.NextPage = 8
	if err := store.SaveBackfillProgress(ctx, want); err != nil {
		t.Fatalf("second SaveBackfillProgress: %v", err)
	}
	if got, _ := store.LoadBackfillProgress(ctx, "repo-a"); got.NextPage != 8 {
		t.Errorf("NextPage = %d, want the updated 8", got.NextPage)
	}
}

func TestIntegration_ListRepositoryNames(t *testing.T) {
	store, _, ctx := migratedStore(t)

	for i, name := range []string{"zeta", "alpha"} {
		if err := store.UpsertRepository(ctx, Repository{
			ID: int64(i + 1), Name: name, DefaultBranch: "main", Visibility: "private",
			Archived: name == "zeta",
		}); err != nil {
			t.Fatalf("UpsertRepository: %v", err)
		}
	}

	names, err := store.ListRepositoryNames(ctx)
	if err != nil {
		t.Fatalf("ListRepositoryNames: %v", err)
	}
	// Archived repositories are included: they gain no new runs, but they can
	// still hold history inside the retention window.
	if len(names) != 2 || names[0] != "alpha" || names[1] != "zeta" {
		t.Errorf("names = %v, want [alpha zeta]", names)
	}
}

// The reason the collection horizon sits INSIDE the retention window, proved
// rather than argued.
//
// Migration 003's rollups accumulate, because a raw row passes through them
// exactly once: in the statement that deletes it. Offer the same already-pruned
// run again and its day is counted twice. The old collector walked all of
// history on every poll, so this happened every fifteen minutes -- silently,
// and in the direction that looks like more activity rather than less.
func TestIntegration_ReinsertingAPrunedRunDoubleCountsItsDay(t *testing.T) {
	store, pool, ctx := migratedStore(t)

	old := time.Now().UTC().Add(-100 * 24 * time.Hour)
	insertRun(t, store, ctx, 1, "repo-a", old, &old)

	if queryInt(t, ctx, pool, "SELECT COUNT(*) FROM workflow_runs") != 0 {
		t.Fatal("a run beyond the retention window should have been pruned on insert")
	}
	if got := queryInt(t, ctx, pool, "SELECT runs FROM workflow_runs_daily"); got != 1 {
		t.Fatalf("rollup counted %d runs, want 1", got)
	}

	// Fetch it again, as the unbounded walk did on every cycle.
	insertRun(t, store, ctx, 1, "repo-a", old, &old)

	if got := queryInt(t, ctx, pool, "SELECT runs FROM workflow_runs_daily"); got != 2 {
		t.Fatalf("rollup counted %d runs after a second insert, want 2", got)
	}
	// One run, counted twice, with nothing to say it happened. The horizon is
	// what makes sure the exporter can never be handed this row a second time:
	// CollectionHorizon is inside RetentionWindow, so anything fetchable is
	// newer than anything prunable.
}

// The other half of the same argument: the jobs of a pruned run cannot be
// stored at all, so fetching them is a request spent on nothing.
func TestIntegration_JobsOfAPrunedRunAreDiscarded(t *testing.T) {
	store, pool, ctx := migratedStore(t)

	old := time.Now().UTC().Add(-100 * 24 * time.Hour)
	insertRun(t, store, ctx, 1, "repo-a", old, &old)

	started := old.Add(time.Minute)
	completed := old.Add(2 * time.Minute)
	if err := store.UpsertWorkflowJob(ctx, WorkflowJob{
		ID: 10, RunID: 1, Name: "build", Conclusion: "success",
		StartedAt: &started, CompletedAt: &completed,
	}); err != nil {
		t.Fatalf("UpsertWorkflowJob: %v", err)
	}

	if got := queryInt(t, ctx, pool, "SELECT COUNT(*) FROM workflow_jobs"); got != 0 {
		t.Errorf("stored %d jobs for a pruned run, want 0", got)
	}
}

// The migration itself: the column and the table the backfiller needs, and the
// partial index that makes the pending query cheap.
func TestIntegration_BackfillSchema(t *testing.T) {
	_, pool, ctx := migratedStore(t)

	if got := queryInt(t, ctx, pool, `
		SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_name = 'workflow_runs' AND column_name = 'jobs_synced_at'`); got != 1 {
		t.Error("workflow_runs.jobs_synced_at is missing")
	}
	if got := queryInt(t, ctx, pool, `
		SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_name = 'backfill_progress'`); got != 1 {
		t.Error("backfill_progress is missing")
	}
	if got := queryInt(t, ctx, pool, `
		SELECT COUNT(*) FROM pg_indexes
		 WHERE indexname = 'idx_workflow_runs_jobs_pending'`); got != 1 {
		t.Error("the partial index on pending job fetches is missing")
	}
}

// Marking a run's jobs as synced is an UPDATE, and the retention triggers fire
// on INSERT. If that ever changed, every mark would run four prunes.
func TestIntegration_MarkingJobsSyncedDoesNotPrune(t *testing.T) {
	store, pool, ctx := migratedStore(t)

	now := time.Now().UTC()
	insertRun(t, store, ctx, 1, "repo-a", now, &now)
	if err := store.MarkJobsSynced(ctx, 1, now); err != nil {
		t.Fatalf("MarkJobsSynced: %v", err)
	}

	if got := queryInt(t, ctx, pool, "SELECT COUNT(*) FROM workflow_runs"); got != 1 {
		t.Errorf("workflow_runs holds %d rows after a mark, want 1", got)
	}
}
