package db

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
)

func TestLoadMigrations_ReturnsFiles(t *testing.T) {
	migs, err := LoadMigrations()
	if err != nil {
		t.Fatalf("LoadMigrations() error: %v", err)
	}
	if len(migs) == 0 {
		t.Fatal("expected at least one migration, got 0")
	}
	for _, m := range migs {
		if m.Name == "" {
			t.Error("migration has empty name")
		}
		if m.SQL == "" {
			t.Errorf("migration %s has empty SQL", m.Name)
		}
	}
}

func TestLoadMigrations_SortedByFilename(t *testing.T) {
	migs, err := LoadMigrations()
	if err != nil {
		t.Fatalf("LoadMigrations() error: %v", err)
	}

	names := make([]string, len(migs))
	for i, m := range migs {
		names[i] = m.Name
	}

	sorted := make([]string, len(names))
	copy(sorted, names)
	sort.Strings(sorted)

	for i := range names {
		if names[i] != sorted[i] {
			t.Errorf("migration order mismatch at index %d: got %s, want %s", i, names[i], sorted[i])
		}
	}
}

func TestMigrationNamesOrdered(t *testing.T) {
	names, err := MigrationNamesOrdered()
	if err != nil {
		t.Fatalf("MigrationNamesOrdered() error: %v", err)
	}
	if len(names) < 3 {
		t.Fatalf("expected at least 3 migrations, got %d", len(names))
	}
	if names[0] != "001_initial.sql" {
		t.Errorf("first migration = %q, want %q", names[0], "001_initial.sql")
	}
	if names[1] != "002_retention_triggers.sql" {
		t.Errorf("second migration = %q, want %q", names[1], "002_retention_triggers.sql")
	}
	if names[2] != "003_retention_correctness.sql" {
		t.Errorf("third migration = %q, want %q", names[2], "003_retention_correctness.sql")
	}
}

func TestMigrationFilenamesHaveSequentialPrefixes(t *testing.T) {
	names, err := MigrationNamesOrdered()
	if err != nil {
		t.Fatalf("MigrationNamesOrdered() error: %v", err)
	}
	for i, name := range names {
		expected := fmt.Sprintf("%03d_", i+1)
		if !strings.HasPrefix(name, expected) {
			t.Errorf("migration %d: name %q does not start with %q", i, name, expected)
		}
	}
}

func TestMigrationSQL_ContainsExpectedStatements(t *testing.T) {
	migs, err := LoadMigrations()
	if err != nil {
		t.Fatalf("LoadMigrations() error: %v", err)
	}

	// 001_initial.sql should contain CREATE TABLE for all raw and rollup tables.
	initial := migs[0]
	expectedTables := []string{
		"repositories",
		"workflow_runs",
		"workflow_jobs",
		"pull_requests",
		"commits",
		"tags",
		"workflow_runs_daily",
		"workflow_jobs_daily",
		"commits_daily",
		"pull_requests_daily",
	}
	for _, table := range expectedTables {
		if !strings.Contains(initial.SQL, table) {
			t.Errorf("001_initial.sql missing table %q", table)
		}
	}

	// 002_retention_triggers.sql should contain trigger and function definitions.
	retention := migs[1]
	expectedKeywords := []string{
		"CREATE OR REPLACE FUNCTION",
		"CREATE TRIGGER",
		"AFTER INSERT",
		"FOR EACH STATEMENT",
		"INTERVAL '90 days'",
	}
	for _, kw := range expectedKeywords {
		if !strings.Contains(retention.SQL, kw) {
			t.Errorf("002_retention_triggers.sql missing keyword %q", kw)
		}
	}
}

func TestMigrationSQL_ValidSyntax(t *testing.T) {
	// Validate basic SQL structure: each migration should have balanced
	// parentheses and end with a semicolon (ignoring trailing whitespace).
	migs, err := LoadMigrations()
	if err != nil {
		t.Fatalf("LoadMigrations() error: %v", err)
	}
	for _, m := range migs {
		sql := strings.TrimSpace(m.SQL)
		if sql == "" {
			t.Errorf("migration %s is empty", m.Name)
			continue
		}
		if !strings.HasSuffix(sql, ";") {
			t.Errorf("migration %s does not end with semicolon", m.Name)
		}

		// Check balanced parentheses (accounting for $$ blocks).
		depth := 0
		inDollarQuote := false
		for i := 0; i < len(sql); i++ {
			if i < len(sql)-1 && sql[i] == '$' && sql[i+1] == '$' {
				inDollarQuote = !inDollarQuote
				i++ // skip second $
				continue
			}
			if inDollarQuote {
				continue
			}
			switch sql[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
		}
		if depth != 0 {
			t.Errorf("migration %s has unbalanced parentheses (depth=%d)", m.Name, depth)
		}
	}
}

func TestRunMigrations_AppliesFromEmpty(t *testing.T) {
	pool := newMockPool()
	ctx := context.Background()

	err := RunMigrations(ctx, pool)
	if err != nil {
		t.Fatalf("RunMigrations() error: %v", err)
	}

	calls := pool.getExecCalls()
	if len(calls) == 0 {
		t.Fatal("expected Exec calls, got 0")
	}

	// First call should be the schema_migrations table creation.
	if !strings.Contains(calls[0].SQL, "schema_migrations") {
		t.Errorf("first Exec call should create schema_migrations, got: %s", calls[0].SQL[:80])
	}

	// One application plus one recording per migration -- counted from the
	// embedded set rather than hardcoded, so adding a migration does not make
	// this test lie about which number it expected.
	want, err := MigrationNamesOrdered()
	if err != nil {
		t.Fatalf("MigrationNamesOrdered() error: %v", err)
	}
	migrationApplied := 0
	migrationRecorded := 0
	for _, c := range calls[1:] {
		if strings.Contains(c.SQL, "INSERT INTO schema_migrations") {
			migrationRecorded++
		} else {
			migrationApplied++
		}
	}
	if migrationApplied != len(want) {
		t.Errorf("expected %d migration applications, got %d", len(want), migrationApplied)
	}
	if migrationRecorded != len(want) {
		t.Errorf("expected %d migration recordings, got %d", len(want), migrationRecorded)
	}
}

func TestRunMigrations_SkipsAlreadyApplied(t *testing.T) {
	pool := newMockPool()
	// Return count=1 for migration checks (all already applied).
	pool.queryRowDefault = &mockRow{values: []any{1}}
	ctx := context.Background()

	err := RunMigrations(ctx, pool)
	if err != nil {
		t.Fatalf("RunMigrations() error: %v", err)
	}

	calls := pool.getExecCalls()
	// Only the schema_migrations table creation should be called.
	if len(calls) != 1 {
		t.Errorf("expected 1 Exec call (schema_migrations only), got %d", len(calls))
	}
}

func TestRunMigrations_ReturnsErrorOnExecFailure(t *testing.T) {
	pool := newMockPool()
	pool.execErr = fmt.Errorf("connection refused")
	ctx := context.Background()

	err := RunMigrations(ctx, pool)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error should contain 'connection refused', got: %v", err)
	}
}

func TestRunMigrations_MigrationExecFails(t *testing.T) {
	// The first Exec creates the schema_migrations table (succeeds).
	// The second Exec applies the first migration SQL (should fail).
	pool := &nthFailPool{failOnCall: 2, failErr: fmt.Errorf("syntax error")}
	ctx := context.Background()

	err := RunMigrations(ctx, pool)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "applying migration") {
		t.Errorf("error should mention 'applying migration', got: %v", err)
	}
}

func TestRunMigrations_RecordingFails(t *testing.T) {
	// Exec calls: 1=create table (ok), 2=apply migration (ok), 3=record (fail).
	pool := &nthFailPool{failOnCall: 3, failErr: fmt.Errorf("insert error")}
	ctx := context.Background()

	err := RunMigrations(ctx, pool)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "recording migration") {
		t.Errorf("error should mention 'recording migration', got: %v", err)
	}
}

func TestRunMigrations_CheckMigrationError(t *testing.T) {
	// QueryRow returns an error to trigger the "checking migration" error path.
	pool := &scanFailPool{scanErr: fmt.Errorf("scan failed")}
	ctx := context.Background()

	err := RunMigrations(ctx, pool)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "checking migration") {
		t.Errorf("error should mention 'checking migration', got: %v", err)
	}
}

func TestRunMigrations_IdempotentFromEmpty(t *testing.T) {
	// Running migrations twice should produce the same set of Exec calls
	// on the second run (all skipped).
	pool := newMockPool()
	ctx := context.Background()

	// First run: apply all.
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("first RunMigrations() error: %v", err)
	}
	firstCallCount := len(pool.getExecCalls())

	// Now simulate that all migrations are applied.
	pool.queryRowDefault = &mockRow{values: []any{1}}
	pool.mu.Lock()
	pool.execCalls = nil
	pool.mu.Unlock()

	// Second run: skip all.
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("second RunMigrations() error: %v", err)
	}
	secondCallCount := len(pool.getExecCalls())

	if secondCallCount >= firstCallCount {
		t.Errorf("second run should have fewer Exec calls: first=%d, second=%d",
			firstCallCount, secondCallCount)
	}
}

// nthFailPool is a DBPool that fails on the Nth Exec call.
type nthFailPool struct {
	callCount  int
	failOnCall int
	failErr    error
}

func (p *nthFailPool) Exec(_ context.Context, _ string, _ ...any) (CommandTag, error) {
	p.callCount++
	if p.callCount == p.failOnCall {
		return nil, p.failErr
	}
	return mockCommandTag{rowsAffected: 1}, nil
}

func (p *nthFailPool) Query(_ context.Context, _ string, _ ...any) (Rows, error) {
	return &mockRows{}, nil
}

func (p *nthFailPool) QueryRow(_ context.Context, _ string, _ ...any) Row {
	return &mockRow{values: []any{0}} // count=0, not yet applied
}

// scanFailPool is a DBPool whose QueryRow always returns a row that fails on Scan.
type scanFailPool struct {
	scanErr error
}

func (p *scanFailPool) Exec(_ context.Context, _ string, _ ...any) (CommandTag, error) {
	return mockCommandTag{rowsAffected: 1}, nil
}

func (p *scanFailPool) Query(_ context.Context, _ string, _ ...any) (Rows, error) {
	return &mockRows{}, nil
}

func (p *scanFailPool) QueryRow(_ context.Context, _ string, _ ...any) Row {
	return &mockRow{err: p.scanErr}
}
