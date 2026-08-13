package db

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSampleOpenPullRequests_UsesGivenDay(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	day := time.Date(2026, 3, 15, 14, 30, 0, 0, time.UTC)
	if err := store.SampleOpenPullRequests(ctx, day); err != nil {
		t.Fatalf("SampleOpenPullRequests() error: %v", err)
	}

	calls := pool.getExecCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 Exec call, got %d", len(calls))
	}
	if !strings.Contains(calls[0].SQL, "pull_requests_daily") {
		t.Errorf("statement should target pull_requests_daily, got: %s", calls[0].SQL)
	}
	if len(calls[0].Args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(calls[0].Args))
	}
	if got := calls[0].Args[0]; got != "2026-03-15" {
		t.Errorf("day arg = %v, want 2026-03-15", got)
	}
}

func TestSampleOpenPullRequests_WrapsError(t *testing.T) {
	pool := newMockPool()
	pool.execErr = fmt.Errorf("connection reset")
	store := NewStore(pool)

	err := store.SampleOpenPullRequests(context.Background(),
		time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "2026-03-15") {
		t.Errorf("error should name the day, got: %v", err)
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("error should wrap the cause, got: %v", err)
	}
}

// The sample has to be reconstructible rather than a snapshot of the current
// state column, otherwise re-sampling yesterday after midnight would stamp
// today's open set onto it.
func TestSampleOpenPullRequestsSQL_ReconstructsFromTimestamps(t *testing.T) {
	required := []string{
		"created_at <",
		"closed_at IS NULL OR",
		"LEFT JOIN",
		"ON CONFLICT (day, repo) DO UPDATE",
		"open_at_eod = EXCLUDED.open_at_eod",
	}
	for _, frag := range required {
		if !strings.Contains(SampleOpenPullRequestsSQL, frag) {
			t.Errorf("sample SQL missing %q", frag)
		}
	}
	// A count of currently-open rows would need the state column; reading it
	// here would mean the answer changes after the fact.
	if strings.Contains(SampleOpenPullRequestsSQL, "state = 'open'") {
		t.Error("sample SQL must not read the state column")
	}
	// It must never touch a column the prune triggers own.
	for _, owned := range []string{"opened", "merged", "closed", "time_to_merge"} {
		if strings.Contains(SampleOpenPullRequestsSQL, owned+" =") {
			t.Errorf("sample SQL writes %q, which the retention triggers own", owned)
		}
	}
}

func TestRetentionTriggerSQL_RollupBeforePrune(t *testing.T) {
	// Each retention function must aggregate into the daily table before it
	// deletes from the raw one, or the ninetieth day is lost at the boundary.
	sql := retentionSQL(t)

	functions := []struct {
		name       string
		dailyTable string
		rawTable   string
	}{
		{"fn_retain_workflow_runs", "workflow_runs_daily", "workflow_runs"},
		{"fn_retain_workflow_jobs", "workflow_jobs_daily", "workflow_jobs"},
		{"fn_retain_pull_requests", "pull_requests_daily", "pull_requests"},
		{"fn_retain_commits", "commits_daily", "commits"},
	}

	for _, fn := range functions {
		t.Run(fn.name, func(t *testing.T) {
			body := functionBody(t, sql, fn.name)
			insertIdx := strings.Index(body, "INSERT INTO "+fn.dailyTable)
			deleteIdx := strings.Index(body, "DELETE FROM "+fn.rawTable)
			if insertIdx == -1 {
				t.Fatalf("INSERT INTO %s not found in %s", fn.dailyTable, fn.name)
			}
			if deleteIdx == -1 {
				t.Fatalf("DELETE FROM %s not found in %s", fn.rawTable, fn.name)
			}
			if insertIdx >= deleteIdx {
				t.Errorf("%s: rollup must come before prune", fn.name)
			}
		})
	}
}

// Deleting a run cascades into its jobs. If the run function does not aggregate
// them on the way past, workflow_jobs_daily is emptied by the very prune it
// exists to survive.
func TestRetentionTriggerSQL_RunPruneRollsUpItsJobs(t *testing.T) {
	body := functionBody(t, retentionSQL(t), "fn_retain_workflow_runs")

	jobsIdx := strings.Index(body, "INSERT INTO workflow_jobs_daily")
	runsIdx := strings.Index(body, "INSERT INTO workflow_runs_daily")
	deleteIdx := strings.Index(body, "DELETE FROM workflow_runs")

	if jobsIdx == -1 {
		t.Fatal("fn_retain_workflow_runs must roll up workflow_jobs_daily before the cascade")
	}
	if jobsIdx >= runsIdx || runsIdx >= deleteIdx {
		t.Errorf("expected jobs rollup, then runs rollup, then delete; got %d, %d, %d",
			jobsIdx, runsIdx, deleteIdx)
	}
}

// A day whose rows are pruned by more than one statement -- every day of the
// first backfill, because the collectors page -- must accumulate. Overwriting
// keeps only the last statement's rows.
func TestRetentionTriggerSQL_AccumulatesRatherThanOverwrites(t *testing.T) {
	sql := retentionSQL(t)

	cases := []struct{ table, column string }{
		{"workflow_runs_daily", "runs"},
		{"workflow_runs_daily", "passed"},
		{"workflow_runs_daily", "failed"},
		{"workflow_runs_daily", "cancelled"},
		{"workflow_jobs_daily", "runs"},
		{"commits_daily", "count"},
		{"pull_requests_daily", "opened"},
		{"pull_requests_daily", "merged"},
		{"pull_requests_daily", "closed"},
	}
	for _, c := range cases {
		t.Run(c.table+"."+c.column, func(t *testing.T) {
			want := fmt.Sprintf("%s = %s.%s + EXCLUDED.%s", c.column, c.table, c.column, c.column)
			if !strings.Contains(sql, want) {
				t.Errorf("retention SQL missing accumulating update %q", want)
			}
		})
	}
}

// open_at_eod is the exporter's column. If a retention function assigned it,
// the two writers would fight and the loser would be whichever ran last.
func TestRetentionTriggerSQL_LeavesOpenAtEODAlone(t *testing.T) {
	if strings.Contains(retentionSQL(t), "open_at_eod =") {
		t.Error("retention triggers must not write open_at_eod")
	}
}

// The prune filters on these columns on every insert statement. Unindexed, each
// one is a sequential scan.
func TestMigrationSQL_PrunedColumnsAreIndexed(t *testing.T) {
	all := allMigrationSQL(t)

	indexed := []struct{ table, column string }{
		{"workflow_runs", "created_at"},
		{"workflow_jobs", "started_at"},
		{"pull_requests", "closed_at"},
		{"commits", "committed_at"},
	}
	for _, i := range indexed {
		t.Run(i.table+"."+i.column, func(t *testing.T) {
			want := fmt.Sprintf("ON %s (%s)", i.table, i.column)
			if !strings.Contains(all, want) {
				t.Errorf("no index on pruned column: expected %q", want)
			}
		})
	}
}

// retentionSQL returns the SQL of the migration that currently defines the
// retention functions.
func retentionSQL(t *testing.T) string {
	t.Helper()
	migs, err := LoadMigrations()
	if err != nil {
		t.Fatalf("LoadMigrations() error: %v", err)
	}
	// The last migration to define a retention function wins, since CREATE OR
	// REPLACE means later definitions supersede earlier ones.
	var out string
	for _, m := range migs {
		if strings.Contains(m.SQL, "fn_retain_workflow_runs()") {
			out = m.SQL
		}
	}
	if out == "" {
		t.Fatal("no migration defines the retention functions")
	}
	return out
}

func allMigrationSQL(t *testing.T) string {
	t.Helper()
	migs, err := LoadMigrations()
	if err != nil {
		t.Fatalf("LoadMigrations() error: %v", err)
	}
	var b strings.Builder
	for _, m := range migs {
		b.WriteString(m.SQL)
	}
	return b.String()
}

// functionBody returns the text from a function's definition up to the start of
// the next one, so an assertion about ordering cannot accidentally match a
// statement belonging to a different function.
func functionBody(t *testing.T, sql, name string) string {
	t.Helper()
	start := strings.Index(sql, "FUNCTION "+name+"()")
	if start == -1 {
		t.Fatalf("function %s not found", name)
	}
	rest := sql[start:]
	if end := strings.Index(rest[1:], "CREATE OR REPLACE FUNCTION"); end != -1 {
		rest = rest[:end+1]
	}
	return rest
}
