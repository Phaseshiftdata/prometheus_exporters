package db

import (
	"context"
	"fmt"
	"testing"
)

// TestIntegration_SchemaShapeAfterEachMigration applies each migration one at
// a time to a fresh database and asserts the expected objects exist at every
// step. This is the obligation coverage counterpart for SQL migrations: every
// migration must produce a verifiable schema shape, because line coverage
// cannot instrument SQL executed inside PostgreSQL.
func TestIntegration_SchemaShapeAfterEachMigration(t *testing.T) {
	pool, ctx := newTestDB(t)

	migs, err := LoadMigrations()
	if err != nil {
		t.Fatalf("LoadMigrations() error: %v", err)
	}

	mustExec(t, ctx, pool, createMigrationsTableSQL)

	// After each migration, assert that the objects it is known to create
	// actually exist. This catches migrations that silently fail (e.g., a
	// CREATE IF NOT EXISTS that references a wrong column type) and guards
	// against regressions where a later migration drops something.
	for i, m := range migs {
		mustExec(t, ctx, pool, m.SQL)
		mustExec(t, ctx, pool, "INSERT INTO schema_migrations (name) VALUES ($1)", m.Name)

		switch i {
		case 0: // 001_initial.sql
			assertTablesExist(t, ctx, pool, []string{
				"repositories", "workflow_runs", "workflow_jobs",
				"pull_requests", "commits", "tags",
				"workflow_runs_daily", "workflow_jobs_daily",
				"commits_daily", "pull_requests_daily",
			})
			assertIndicesExist(t, ctx, pool, []string{
				"idx_workflow_runs_created",
				"idx_workflow_runs_repo",
				"idx_workflow_jobs_run",
				"idx_pull_requests_created",
				"idx_pull_requests_repo",
				"idx_commits_committed",
				"idx_commits_repo",
			})

		case 1: // 002_retention_triggers.sql
			assertTriggersExist(t, ctx, pool, []string{
				"trg_retain_workflow_runs",
				"trg_retain_workflow_jobs",
				"trg_retain_pull_requests",
				"trg_retain_commits",
			})
			assertFunctionsExist(t, ctx, pool, []string{
				"fn_retain_workflow_runs",
				"fn_retain_workflow_jobs",
				"fn_retain_pull_requests",
				"fn_retain_commits",
			})

		case 2: // 003_retention_correctness.sql
			assertIndicesExist(t, ctx, pool, []string{
				"idx_workflow_jobs_started",
				"idx_pull_requests_closed",
			})
			assertFunctionsExist(t, ctx, pool, []string{
				"fn_blend_percentile",
			})

		case 3: // 004_backfill_progress.sql
			assertTablesExist(t, ctx, pool, []string{
				"backfill_progress",
			})
			assertColumnsExist(t, ctx, pool, "workflow_runs", []string{
				"jobs_synced_at",
			})
			assertIndicesExist(t, ctx, pool, []string{
				"idx_workflow_runs_jobs_pending",
			})
		}
	}
}

func assertTablesExist(t *testing.T, ctx context.Context, pool DBPool, tables []string) {
	t.Helper()
	for _, table := range tables {
		got := queryInt(t, ctx, pool,
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_name=$1",
			table)
		if got != 1 {
			t.Errorf("table %s does not exist after migration", table)
		}
	}
}

func assertIndicesExist(t *testing.T, ctx context.Context, pool DBPool, indices []string) {
	t.Helper()
	for _, idx := range indices {
		got := queryInt(t, ctx, pool,
			"SELECT COUNT(*) FROM pg_indexes WHERE schemaname='public' AND indexname=$1",
			idx)
		if got != 1 {
			t.Errorf("index %s does not exist after migration", idx)
		}
	}
}

func assertTriggersExist(t *testing.T, ctx context.Context, pool DBPool, triggers []string) {
	t.Helper()
	for _, trg := range triggers {
		got := queryInt(t, ctx, pool, fmt.Sprintf(`
			SELECT COUNT(*) FROM pg_trigger t
			  JOIN pg_class c ON c.oid = t.tgrelid
			  JOIN pg_namespace n ON n.oid = c.relnamespace
			 WHERE n.nspname = current_schema() AND t.tgname = '%s'`, trg))
		if got != 1 {
			t.Errorf("trigger %s does not exist after migration", trg)
		}
	}
}

func assertFunctionsExist(t *testing.T, ctx context.Context, pool DBPool, functions []string) {
	t.Helper()
	for _, fn := range functions {
		got := queryInt(t, ctx, pool, fmt.Sprintf(`
			SELECT COUNT(*) FROM pg_proc p
			  JOIN pg_namespace n ON n.oid = p.pronamespace
			 WHERE n.nspname = current_schema() AND p.proname = '%s'`, fn))
		if got < 1 {
			t.Errorf("function %s does not exist after migration", fn)
		}
	}
}

func assertColumnsExist(t *testing.T, ctx context.Context, pool DBPool, table string, columns []string) {
	t.Helper()
	for _, col := range columns {
		got := queryInt(t, ctx, pool,
			"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND column_name=$2",
			table, col)
		if got != 1 {
			t.Errorf("column %s.%s does not exist after migration", table, col)
		}
	}
}
