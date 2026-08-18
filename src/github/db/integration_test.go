package db

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Issue #26 asks for two things to be proved rather than argued: that applying
// the migrations from an empty database lands in the same place as applying
// them incrementally, and that a row on the ninetieth day is pruned while its
// day survives in the rollup with the correct counts. Neither can be shown
// against a mock -- the behavior under test lives in PostgreSQL, in triggers
// and in ON CONFLICT clauses, and a mock that records SQL strings would happily
// agree with a migration that loses every row it touches. Three of the defects
// migration 003 fixes were found by these tests and by nothing else.
//
// Each test gets its own database. Schemas would be cheaper, but search_path is
// per connection and the pool hands out several, so a test could see a
// half-migrated schema for reasons that have nothing to do with what it is
// testing.

var testDBCounter atomic.Int64

// newTestDB creates an empty database and returns a pool connected to it. The
// test is skipped, not failed, when no PostgreSQL is reachable -- the same
// contract the rest of this package's database tests follow.
func newTestDB(t *testing.T) (*PgxPool, context.Context) {
	t.Helper()
	ctx := context.Background()

	admin, err := Connect(ctx, testDSN())
	if err != nil {
		t.Skipf("skipping integration test, cannot connect to PostgreSQL: %v", err)
	}

	name := fmt.Sprintf("gh_it_%d_%d", time.Now().UnixNano(), testDBCounter.Add(1))
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		admin.Close()
		t.Fatalf("creating test database: %v", err)
	}

	dsn, err := dsnForDatabase(testDSN(), name)
	if err != nil {
		admin.Close()
		t.Fatalf("building DSN: %v", err)
	}

	pool, err := Connect(ctx, dsn)
	if err != nil {
		admin.Close()
		t.Fatalf("connecting to test database: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		// FORCE because the pool's connections may not have gone yet, and a
		// leftover database would make the next run's name collision noisy.
		if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Logf("dropping test database %s: %v", name, err)
		}
		admin.Close()
	})

	return pool, ctx
}

// dsnForDatabase rewrites the database name in a PostgreSQL URL.
func dsnForDatabase(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}

func TestDSNForDatabase(t *testing.T) {
	got, err := dsnForDatabase("postgres://u:p@host:5432/testdb?sslmode=disable", "other")
	if err != nil {
		t.Fatalf("dsnForDatabase() error: %v", err)
	}
	if want := "postgres://u:p@host:5432/other?sslmode=disable"; got != want {
		t.Errorf("dsnForDatabase() = %q, want %q", got, want)
	}
	if _, err := dsnForDatabase("://not a url", "other"); err == nil {
		t.Error("expected an error for an unparseable DSN")
	}
}

// schemaSnapshot renders everything about the schema that a migration can
// change, as sorted text. Two databases with equal snapshots are
// indistinguishable to anything that queries them.
const schemaSnapshotSQL = `
SELECT string_agg(line, E'\n' ORDER BY line) FROM (
    SELECT 'column:' || table_name || '.' || column_name || ':' || data_type ||
           ':' || is_nullable || ':' || COALESCE(column_default, '-') AS line
      FROM information_schema.columns
     WHERE table_schema = current_schema()
    UNION ALL
    SELECT 'index:' || indexname || ':' || indexdef
      FROM pg_indexes WHERE schemaname = current_schema()
    UNION ALL
    SELECT 'trigger:' || c.relname || ':' || t.tgname || ':' || t.tgtype::text
      FROM pg_trigger t
      JOIN pg_class c ON c.oid = t.tgrelid
      JOIN pg_namespace n ON n.oid = c.relnamespace
     WHERE n.nspname = current_schema() AND NOT t.tgisinternal
    UNION ALL
    SELECT 'function:' || p.proname || ':' || md5(p.prosrc)
      FROM pg_proc p
      JOIN pg_namespace n ON n.oid = p.pronamespace
     WHERE n.nspname = current_schema()
    UNION ALL
    SELECT 'constraint:' || c.conname || ':' || pg_get_constraintdef(c.oid)
      FROM pg_constraint c
      JOIN pg_namespace n ON n.oid = c.connamespace
     WHERE n.nspname = current_schema()
) AS s`

func schemaSnapshot(t *testing.T, ctx context.Context, pool DBPool) string {
	t.Helper()
	var snap string
	if err := pool.QueryRow(ctx, schemaSnapshotSQL).Scan(&snap); err != nil {
		t.Fatalf("snapshotting schema: %v", err)
	}
	return snap
}

func queryInt(t *testing.T, ctx context.Context, pool DBPool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	return n
}

func mustExec(t *testing.T, ctx context.Context, pool DBPool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func TestIntegration_MigrationsApplyFromEmpty(t *testing.T) {
	pool, ctx := newTestDB(t)

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations() error: %v", err)
	}

	names, err := MigrationNamesOrdered()
	if err != nil {
		t.Fatalf("MigrationNamesOrdered() error: %v", err)
	}
	if got := queryInt(t, ctx, pool, "SELECT COUNT(*) FROM schema_migrations"); got != len(names) {
		t.Errorf("schema_migrations has %d rows, want %d", got, len(names))
	}

	tables := []string{
		"repositories", "workflow_runs", "workflow_jobs", "pull_requests",
		"commits", "tags", "workflow_runs_daily", "workflow_jobs_daily",
		"commits_daily", "pull_requests_daily",
	}
	for _, table := range tables {
		got := queryInt(t, ctx, pool,
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_name=$1",
			table)
		if got != 1 {
			t.Errorf("table %s does not exist", table)
		}
	}
}

// A row-level trigger fires once per row, so a backfill of twenty-five
// repositories would run every prune thousands of times inside one transaction.
// tgtype's low bit is TRIGGER_TYPE_ROW; it has to be clear on all four.
func TestIntegration_RetentionTriggersAreStatementLevel(t *testing.T) {
	pool, ctx := newTestDB(t)
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations() error: %v", err)
	}

	rowLevel := queryInt(t, ctx, pool, `
		SELECT COUNT(*) FROM pg_trigger
		 WHERE NOT tgisinternal AND (tgtype & 1) <> 0`)
	if rowLevel != 0 {
		t.Errorf("%d retention trigger(s) are FOR EACH ROW, want 0", rowLevel)
	}

	statementLevel := queryInt(t, ctx, pool, `
		SELECT COUNT(*) FROM pg_trigger WHERE NOT tgisinternal`)
	if statementLevel != 4 {
		t.Errorf("found %d retention triggers, want 4", statementLevel)
	}
}

// Every column a prune filters on needs an index, or the DELETE scans the whole
// table on every insert statement.
func TestIntegration_PrunedColumnsAreIndexed(t *testing.T) {
	pool, ctx := newTestDB(t)
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations() error: %v", err)
	}

	for _, c := range []struct{ table, column string }{
		{"workflow_runs", "created_at"},
		{"workflow_jobs", "started_at"},
		{"pull_requests", "closed_at"},
		{"commits", "committed_at"},
	} {
		got := queryInt(t, ctx, pool, `
			SELECT COUNT(*) FROM pg_indexes
			 WHERE schemaname = current_schema()
			   AND tablename = $1
			   AND indexdef LIKE '%(' || $2 || ')'`, c.table, c.column)
		if got == 0 {
			t.Errorf("no index on %s.%s, which the prune filters on", c.table, c.column)
		}
	}
}

// The equivalence issue #26 asks for. For every prefix of the migration list,
// apply that prefix by hand -- standing in for a database that stopped at that
// version -- then let RunMigrations carry it the rest of the way, and require
// the result to be indistinguishable from a database migrated from empty.
func TestIntegration_IncrementalEqualsFromEmpty(t *testing.T) {
	fromEmpty, ctx := newTestDB(t)
	if err := RunMigrations(ctx, fromEmpty); err != nil {
		t.Fatalf("RunMigrations() from empty: %v", err)
	}
	want := schemaSnapshot(t, ctx, fromEmpty)
	if want == "" {
		t.Fatal("schema snapshot is empty; the snapshot query is not measuring anything")
	}

	migs, err := LoadMigrations()
	if err != nil {
		t.Fatalf("LoadMigrations() error: %v", err)
	}

	for cut := 1; cut < len(migs); cut++ {
		t.Run(fmt.Sprintf("resume_after_%s", migs[cut-1].Name), func(t *testing.T) {
			pool, ctx := newTestDB(t)

			mustExec(t, ctx, pool, createMigrationsTableSQL)
			for _, m := range migs[:cut] {
				mustExec(t, ctx, pool, m.SQL)
				mustExec(t, ctx, pool, "INSERT INTO schema_migrations (name) VALUES ($1)", m.Name)
			}

			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatalf("RunMigrations() resuming after %s: %v", migs[cut-1].Name, err)
			}

			if got := schemaSnapshot(t, ctx, pool); got != want {
				t.Errorf("schema differs from a database migrated from empty:\n%s",
					diffLines(want, got))
			}
		})
	}
}

// Forward-only means re-running must be a no-op, not a second application.
func TestIntegration_MigrationsAreIdempotent(t *testing.T) {
	pool, ctx := newTestDB(t)

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("first RunMigrations(): %v", err)
	}
	first := schemaSnapshot(t, ctx, pool)

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("second RunMigrations(): %v", err)
	}
	if second := schemaSnapshot(t, ctx, pool); second != first {
		t.Errorf("re-running the migrations changed the schema:\n%s", diffLines(first, second))
	}

	names, _ := MigrationNamesOrdered()
	if got := queryInt(t, ctx, pool, "SELECT COUNT(*) FROM schema_migrations"); got != len(names) {
		t.Errorf("schema_migrations has %d rows after two runs, want %d", got, len(names))
	}
}

func diffLines(want, got string) string {
	wantSet := map[string]bool{}
	for _, l := range strings.Split(want, "\n") {
		wantSet[l] = true
	}
	gotSet := map[string]bool{}
	for _, l := range strings.Split(got, "\n") {
		gotSet[l] = true
	}
	var b strings.Builder
	for _, l := range strings.Split(want, "\n") {
		if !gotSet[l] {
			fmt.Fprintf(&b, "  missing: %s\n", l)
		}
	}
	for _, l := range strings.Split(got, "\n") {
		if !wantSet[l] {
			fmt.Fprintf(&b, "  unexpected: %s\n", l)
		}
	}
	return b.String()
}

// migratedStore returns a Store over a freshly migrated database.
func migratedStore(t *testing.T) (*Store, DBPool, context.Context) {
	t.Helper()
	pool, ctx := newTestDB(t)
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations(): %v", err)
	}
	return NewStore(pool), pool, ctx
}

func ptrTime(t time.Time) *time.Time { return &t }

// The retention test issue #26 spells out: a row dated ninety-one days ago is
// gone from the raw table, and its day is still in the rollup with the right
// counts. Both rows are inserted by separate statements, so this also covers
// the backfill paging case that overwriting used to lose.
func TestIntegration_RetentionBoundary(t *testing.T) {
	store, pool, ctx := migratedStore(t)

	old := time.Now().AddDate(0, 0, -91)
	day := old.Format("2006-01-02")

	if err := store.UpsertWorkflowRun(ctx, WorkflowRun{
		ID: 1, Repo: "org/repo", Workflow: "ci.yml", Conclusion: "success",
		CreatedAt: old, RunStartedAt: ptrTime(old.Add(10 * time.Second)),
		UpdatedAt: ptrTime(old.Add(70 * time.Second)),
	}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := store.UpsertWorkflowRun(ctx, WorkflowRun{
		ID: 2, Repo: "org/repo", Workflow: "ci.yml", Conclusion: "failure",
		CreatedAt: old, RunStartedAt: ptrTime(old.Add(30 * time.Second)),
		UpdatedAt: ptrTime(old.Add(90 * time.Second)),
	}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if n := queryInt(t, ctx, pool, "SELECT COUNT(*) FROM workflow_runs"); n != 0 {
		t.Errorf("%d raw run(s) survived the prune, want 0", n)
	}

	var runs, passed, failed int
	err := pool.QueryRow(ctx,
		"SELECT runs, passed, failed FROM workflow_runs_daily WHERE day = $1::date", day,
	).Scan(&runs, &passed, &failed)
	if err != nil {
		t.Fatalf("the pruned day is not in the rollup at all: %v", err)
	}
	if runs != 2 || passed != 1 || failed != 1 {
		t.Errorf("rollup for %s = runs %d, passed %d, failed %d; want 2, 1, 1",
			day, runs, passed, failed)
	}

	// Durations are derived from the timestamps at rollup time; the columns
	// exist so that the trend outlives the rows it was computed from.
	exec := queryInt(t, ctx, pool,
		"SELECT COALESCE(exec_max_s, 0)::int FROM workflow_runs_daily WHERE day = $1::date", day)
	if exec != 60 {
		t.Errorf("exec_max_s = %d, want 60", exec)
	}
}

func TestIntegration_RecentRowsAreNotPruned(t *testing.T) {
	store, pool, ctx := migratedStore(t)

	recent := time.Now().AddDate(0, 0, -10)
	for i, conclusion := range []string{"success", "failure"} {
		if err := store.UpsertWorkflowRun(ctx, WorkflowRun{
			ID: int64(i + 1), Repo: "org/repo", Workflow: "ci.yml",
			Conclusion: conclusion, CreatedAt: recent,
		}); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	if n := queryInt(t, ctx, pool, "SELECT COUNT(*) FROM workflow_runs"); n != 2 {
		t.Errorf("%d raw run(s) remain, want 2", n)
	}
	// Nothing has aged out, so nothing should have been compacted yet.
	if n := queryInt(t, ctx, pool, "SELECT COUNT(*) FROM workflow_runs_daily"); n != 0 {
		t.Errorf("%d rollup row(s) written for data still inside the window, want 0", n)
	}
}

// workflow_jobs_daily exists so that slow-build attribution outlives the raw
// rows. Deleting a run cascades into its jobs, so the run prune has to
// aggregate them on the way past -- a trigger on workflow_jobs never sees a
// cascade, only an insert.
func TestIntegration_JobHistorySurvivesTheRunPrune(t *testing.T) {
	store, pool, ctx := migratedStore(t)

	fresh := time.Now().AddDate(0, 0, -10)
	if err := store.UpsertWorkflowRun(ctx, WorkflowRun{
		ID: 1, Repo: "org/repo", Workflow: "ci.yml", Conclusion: "success",
		CreatedAt: fresh,
	}); err != nil {
		t.Fatalf("upsert run: %v", err)
	}
	if err := store.UpsertWorkflowJob(ctx, WorkflowJob{
		ID: 10, RunID: 1, Name: "build", Conclusion: "success",
		StartedAt: ptrTime(fresh), CompletedAt: ptrTime(fresh.Add(300 * time.Second)),
	}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	// Age both past the window, the way real time would.
	mustExec(t, ctx, pool,
		"UPDATE workflow_runs SET created_at = created_at - INTERVAL '81 days'")
	mustExec(t, ctx, pool, `
		UPDATE workflow_jobs
		   SET started_at = started_at - INTERVAL '81 days',
		       completed_at = completed_at - INTERVAL '81 days'`)

	// Any insert fires the statement trigger.
	if err := store.UpsertWorkflowRun(ctx, WorkflowRun{
		ID: 2, Repo: "org/repo", Workflow: "ci.yml", Conclusion: "success",
		CreatedAt: time.Now().AddDate(0, 0, -1),
	}); err != nil {
		t.Fatalf("triggering upsert: %v", err)
	}

	if n := queryInt(t, ctx, pool, "SELECT COUNT(*) FROM workflow_jobs"); n != 0 {
		t.Errorf("%d raw job(s) survived, want 0", n)
	}

	var runs, passed, maxDuration int
	err := pool.QueryRow(ctx, `
		SELECT runs, passed, COALESCE(duration_max_s, 0)::int
		  FROM workflow_jobs_daily WHERE job = 'build'`).Scan(&runs, &passed, &maxDuration)
	if err != nil {
		t.Fatalf("job history was deleted by the cascade instead of rolled up: %v", err)
	}
	if runs != 1 || passed != 1 || maxDuration != 300 {
		t.Errorf("workflow_jobs_daily = runs %d, passed %d, max %ds; want 1, 1, 300s",
			runs, passed, maxDuration)
	}
}

// The collectors page through a repository's whole history, so the first poll
// offers runs that are already past the window. Each is rolled up and deleted
// by its own insert, and the job that follows has no run to reference. That
// must not abort the batch: the bulk upsert stops at the first error, so one
// ancient run would otherwise cost every job behind it, recent ones included.
func TestIntegration_BackfillOfPrunedRunsDoesNotBreakJobIngest(t *testing.T) {
	store, pool, ctx := migratedStore(t)

	old := time.Now().AddDate(0, 0, -91)
	recent := time.Now().AddDate(0, 0, -1)

	if err := store.UpsertWorkflowRuns(ctx, []WorkflowRun{
		{ID: 1, Repo: "org/repo", Workflow: "ci.yml", Conclusion: "success", CreatedAt: old},
		{ID: 2, Repo: "org/repo", Workflow: "ci.yml", Conclusion: "success", CreatedAt: recent},
	}); err != nil {
		t.Fatalf("upsert runs: %v", err)
	}

	err := store.UpsertWorkflowJobs(ctx, []WorkflowJob{
		{ID: 10, RunID: 1, Name: "build", Conclusion: "success", StartedAt: ptrTime(old)},
		{ID: 11, RunID: 2, Name: "build", Conclusion: "success", StartedAt: ptrTime(recent)},
	})
	if err != nil {
		t.Fatalf("a job whose run was pruned aborted the whole batch: %v", err)
	}

	if n := queryInt(t, ctx, pool, "SELECT COUNT(*) FROM workflow_jobs WHERE id = 11"); n != 1 {
		t.Error("the job behind the pruned one was not stored")
	}
	if n := queryInt(t, ctx, pool, "SELECT COUNT(*) FROM workflow_jobs WHERE id = 10"); n != 0 {
		t.Error("a job was stored for a run that no longer exists")
	}
}

// Re-polling the same run has to update it, not duplicate it: GitHub reports a
// run repeatedly as it moves from queued to completed.
func TestIntegration_IngestIsIdempotent(t *testing.T) {
	store, pool, ctx := migratedStore(t)

	created := time.Now().AddDate(0, 0, -1)
	run := WorkflowRun{
		ID: 1, Repo: "org/repo", Workflow: "ci.yml", Conclusion: "",
		CreatedAt: created,
	}
	if err := store.UpsertWorkflowRun(ctx, run); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	run.Conclusion = "failure"
	if err := store.UpsertWorkflowRun(ctx, run); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if n := queryInt(t, ctx, pool, "SELECT COUNT(*) FROM workflow_runs"); n != 1 {
		t.Errorf("re-polling produced %d rows, want 1", n)
	}
	var conclusion string
	if err := pool.QueryRow(ctx, "SELECT conclusion FROM workflow_runs WHERE id = 1").
		Scan(&conclusion); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if conclusion != "failure" {
		t.Errorf("conclusion = %q, want %q -- the re-poll did not update", conclusion, "failure")
	}

	// The same for a commit, which is keyed by SHA rather than a numeric id.
	c := Commit{SHA: "abc", Repo: "org/repo", Branch: "main", Author: "a",
		Message: "one", CommittedAt: created}
	if err := store.UpsertCommit(ctx, c); err != nil {
		t.Fatalf("first commit upsert: %v", err)
	}
	c.Message = "two"
	if err := store.UpsertCommit(ctx, c); err != nil {
		t.Fatalf("second commit upsert: %v", err)
	}
	if n := queryInt(t, ctx, pool, "SELECT COUNT(*) FROM commits"); n != 1 {
		t.Errorf("re-polling a commit produced %d rows, want 1", n)
	}
}

// open_at_eod cannot be reconstructed from opened and merged counts, so it has
// to be sampled while the rows are still there and then survive them.
func TestIntegration_OpenPullRequestCountIsSampledAndSurvives(t *testing.T) {
	store, pool, ctx := migratedStore(t)

	if err := store.UpsertRepository(ctx, Repository{
		ID: 1, Name: "org/repo", DefaultBranch: "main",
	}); err != nil {
		t.Fatalf("upsert repository: %v", err)
	}

	now := time.Now()
	prs := []PullRequest{
		// Open, and older than the window: current state, never a past event.
		{ID: 1, Repo: "org/repo", Number: 1, State: "open", CreatedAt: now.AddDate(0, 0, -120)},
		// Open and recent.
		{ID: 2, Repo: "org/repo", Number: 2, State: "open", CreatedAt: now.AddDate(0, 0, -2)},
		// Merged long ago: rolled up and pruned.
		{ID: 3, Repo: "org/repo", Number: 3, State: "closed",
			CreatedAt: now.AddDate(0, 0, -100),
			MergedAt:  ptrTime(now.AddDate(0, 0, -95)),
			ClosedAt:  ptrTime(now.AddDate(0, 0, -95))},
	}
	if err := store.UpsertPullRequests(ctx, prs); err != nil {
		t.Fatalf("upsert pull requests: %v", err)
	}

	if err := store.SampleOpenPullRequests(ctx, now); err != nil {
		t.Fatalf("SampleOpenPullRequests(): %v", err)
	}

	openAtEOD := queryInt(t, ctx, pool,
		"SELECT open_at_eod FROM pull_requests_daily WHERE day = CURRENT_DATE AND repo = 'org/repo'")
	if openAtEOD != 2 {
		t.Errorf("open_at_eod = %d, want 2", openAtEOD)
	}

	// An open pull request is not a finished event, so age alone must not
	// delete it -- otherwise every later sample undercounts.
	if n := queryInt(t, ctx, pool, "SELECT COUNT(*) FROM pull_requests WHERE closed_at IS NULL"); n != 2 {
		t.Errorf("%d open pull request(s) remain, want 2", n)
	}
	if n := queryInt(t, ctx, pool, "SELECT COUNT(*) FROM pull_requests WHERE id = 3"); n != 0 {
		t.Error("a pull request closed more than ninety days ago was not pruned")
	}

	// The merge is recorded on the day it merged, not the day it opened, and
	// its time to merge outlives the row.
	mergedDay := now.AddDate(0, 0, -95).Format("2006-01-02")
	var merged, ttm int
	err := pool.QueryRow(ctx, `
		SELECT merged, COALESCE(time_to_merge_p50_s, 0)::int
		  FROM pull_requests_daily WHERE day = $1::date`, mergedDay).Scan(&merged, &ttm)
	if err != nil {
		t.Fatalf("the merge day is not in the rollup: %v", err)
	}
	if merged != 1 {
		t.Errorf("merged = %d on %s, want 1", merged, mergedDay)
	}
	if want := 5 * 24 * 60 * 60; ttm != want {
		t.Errorf("time_to_merge_p50_s = %d, want %d", ttm, want)
	}

	// Sampling again must not disturb the columns the triggers own.
	if err := store.SampleOpenPullRequests(ctx, now); err != nil {
		t.Fatalf("re-sampling: %v", err)
	}
	if got := queryInt(t, ctx, pool,
		"SELECT merged FROM pull_requests_daily WHERE day = $1::date", mergedDay); got != 1 {
		t.Errorf("merged = %d after re-sampling, want 1", got)
	}
}

// A repository with nothing open is a measured zero, not a gap. Sampling a past
// day has to reconstruct it from the timestamps, or a poll landing after
// midnight would stamp this morning's open set onto yesterday.
func TestIntegration_OpenPullRequestSampleIsReconstructedPerDay(t *testing.T) {
	store, pool, ctx := migratedStore(t)

	if err := store.UpsertRepository(ctx, Repository{ID: 1, Name: "org/repo"}); err != nil {
		t.Fatalf("upsert repository: %v", err)
	}
	if err := store.UpsertRepository(ctx, Repository{ID: 2, Name: "org/quiet"}); err != nil {
		t.Fatalf("upsert repository: %v", err)
	}

	now := time.Now()
	// Opened three days ago, closed two days ago: open at the end of day -3,
	// not at the end of day -1.
	if err := store.UpsertPullRequest(ctx, PullRequest{
		ID: 1, Repo: "org/repo", Number: 1, State: "closed",
		CreatedAt: now.AddDate(0, 0, -3),
		ClosedAt:  ptrTime(now.AddDate(0, 0, -2)),
	}); err != nil {
		t.Fatalf("upsert pull request: %v", err)
	}

	for _, c := range []struct {
		offset int
		want   int
	}{{-3, 1}, {-1, 0}} {
		day := now.AddDate(0, 0, c.offset)
		if err := store.SampleOpenPullRequests(ctx, day); err != nil {
			t.Fatalf("SampleOpenPullRequests(%d): %v", c.offset, err)
		}
		got := queryInt(t, ctx, pool,
			"SELECT open_at_eod FROM pull_requests_daily WHERE day = $1::date AND repo = 'org/repo'",
			day.Format("2006-01-02"))
		if got != c.want {
			t.Errorf("open_at_eod on day %d = %d, want %d", c.offset, got, c.want)
		}
	}

	got := queryInt(t, ctx, pool,
		"SELECT open_at_eod FROM pull_requests_daily WHERE repo = 'org/quiet' LIMIT 1")
	if got != 0 {
		t.Errorf("a repository with nothing open recorded %d, want an explicit 0", got)
	}
}

// Commits and their rollup, including the accumulation that backfill paging
// depends on.
func TestIntegration_CommitRetentionAccumulates(t *testing.T) {
	store, pool, ctx := migratedStore(t)

	old := time.Now().AddDate(0, 0, -91)
	for i, sha := range []string{"aaa", "bbb"} {
		if err := store.UpsertCommit(ctx, Commit{
			SHA: sha, Repo: "org/repo", Branch: "main", Author: "alice",
			Message: fmt.Sprintf("c%d", i), CommittedAt: old,
		}); err != nil {
			t.Fatalf("upsert commit: %v", err)
		}
	}

	if n := queryInt(t, ctx, pool, "SELECT COUNT(*) FROM commits"); n != 0 {
		t.Errorf("%d raw commit(s) survived, want 0", n)
	}
	got := queryInt(t, ctx, pool,
		"SELECT count FROM commits_daily WHERE day = $1::date AND author = 'alice'",
		old.Format("2006-01-02"))
	if got != 2 {
		t.Errorf("commits_daily count = %d, want 2 -- separate statements must accumulate", got)
	}
}

// Grafana's role is read-only by grant, not by convention. The exporter's own
// role owns the objects; this asserts the shape the deployment grants rely on,
// namely that everything lives in one schema owned by the connecting role.
func TestIntegration_MigrationsCreateEverythingInOneSchema(t *testing.T) {
	pool, ctx := newTestDB(t)
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations(): %v", err)
	}

	stray := queryInt(t, ctx, pool, `
		SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema NOT IN ('pg_catalog', 'information_schema', current_schema())`)
	if stray != 0 {
		t.Errorf("%d table(s) created outside the schema the grants cover", stray)
	}
}
