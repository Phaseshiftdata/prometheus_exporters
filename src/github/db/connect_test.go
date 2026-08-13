package db

import (
	"context"
	"os"
	"testing"
)

// Compile-time interface checks: PgxPool must satisfy DBPool.
var _ DBPool = (*PgxPool)(nil)

func TestPgxPoolImplementsDBPool(t *testing.T) {
	// This test exists to surface the compile-time check above in test output.
	// If PgxPool does not satisfy DBPool, the file will not compile.
	t.Log("PgxPool satisfies DBPool")
}

// mockTagInner is a minimal mock for the pgxCommandTag inner interface.
type mockTagInner struct {
	rows int64
}

func (m mockTagInner) RowsAffected() int64 { return m.rows }

func TestPgxCommandTag_RowsAffected(t *testing.T) {
	tag := pgxCommandTag{tag: mockTagInner{rows: 42}}
	if got := tag.RowsAffected(); got != 42 {
		t.Errorf("RowsAffected() = %d, want 42", got)
	}
}

// mockRowsInner is a minimal mock for the pgxRows inner interface.
type mockRowsInner struct {
	nextCalls int
	maxCalls  int
	scanErr   error
	errVal    error
}

func (m *mockRowsInner) Next() bool {
	m.nextCalls++
	return m.nextCalls <= m.maxCalls
}

func (m *mockRowsInner) Scan(_ ...any) error { return m.scanErr }
func (m *mockRowsInner) Err() error          { return m.errVal }
func (m *mockRowsInner) Close()              {}

func TestPgxRows_Next(t *testing.T) {
	inner := &mockRowsInner{maxCalls: 2}
	rows := pgxRows{rows: inner}

	count := 0
	for rows.Next() {
		count++
	}
	if count != 2 {
		t.Errorf("Next() iterated %d times, want 2", count)
	}
}

func TestPgxRows_Scan(t *testing.T) {
	inner := &mockRowsInner{maxCalls: 1}
	rows := pgxRows{rows: inner}

	if err := rows.Scan(); err != nil {
		t.Errorf("Scan() error: %v", err)
	}
}

func TestPgxRows_Err(t *testing.T) {
	inner := &mockRowsInner{}
	rows := pgxRows{rows: inner}

	if err := rows.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestPgxRows_Close(t *testing.T) {
	inner := &mockRowsInner{}
	rows := pgxRows{rows: inner}
	// Just verify it doesn't panic.
	rows.Close()
}

// testDSN returns the PostgreSQL DSN from TEST_DATABASE_URL if available.
func testDSN() string {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		// A local fallback so `go test ./...` works against a database started
		// by hand. CI sets TEST_DATABASE_URL explicitly and does not rely on
		// this. Tests that need a database SKIP when neither is reachable --
		// which is exactly why they were invisible in CI until the workflow
		// grew a postgres service.
		dsn = "postgres://postgres:test@127.0.0.1:5432/testdb?sslmode=disable&connect_timeout=2"
	}
	return dsn
}

func TestConnect(t *testing.T) {
	dsn := testDSN()
	ctx := context.Background()

	pool, err := Connect(ctx, dsn)
	if err != nil {
		t.Skipf("skipping integration test, cannot connect to PostgreSQL: %v", err)
	}
	defer pool.Close()

	// Test Exec
	_, err = pool.Exec(ctx, "SELECT 1")
	if err != nil {
		t.Fatalf("Exec() error: %v", err)
	}

	// Test QueryRow
	row := pool.QueryRow(ctx, "SELECT 42")
	var n int
	if err := row.Scan(&n); err != nil {
		t.Fatalf("QueryRow/Scan error: %v", err)
	}
	if n != 42 {
		t.Errorf("expected 42, got %d", n)
	}

	// Test Query
	rows, err := pool.Query(ctx, "SELECT generate_series(1,3)")
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("rows.Scan() error: %v", err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() = %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 rows, got %d", count)
	}
}

func TestConnect_InvalidDSN(t *testing.T) {
	ctx := context.Background()
	_, err := Connect(ctx, "postgres://invalid:1/nonexistent?connect_timeout=1")
	if err == nil {
		t.Fatal("expected error for invalid DSN")
	}
}

func TestConnect_MalformedDSN(t *testing.T) {
	ctx := context.Background()
	// A completely invalid DSN that makes pgxpool.New fail during parsing.
	_, err := Connect(ctx, "not-a-valid-dsn://[[[invalid")
	if err == nil {
		t.Fatal("expected error for malformed DSN")
	}
}

func TestConnect_QueryError(t *testing.T) {
	dsn := testDSN()
	ctx := context.Background()

	pool, err := Connect(ctx, dsn)
	if err != nil {
		t.Skipf("skipping integration test, cannot connect: %v", err)
	}
	defer pool.Close()

	// Query with invalid SQL to trigger the error path.
	_, qErr := pool.Query(ctx, "SELECT * FROM nonexistent_table_xyz_123")
	if qErr == nil {
		t.Error("expected error for invalid table")
	}
}
