package main

import (
	"context"
	"errors"
	"testing"
	"time"

	ghpkg "github.com/phaseshiftdata/prometheus_exporters/src/github"
	ghdb "github.com/phaseshiftdata/prometheus_exporters/src/github/db"
)

// The adapter carries two questions across the type boundary: "have we already
// collected this?", which is what stops the exporter re-asking GitHub for
// thousands of jobs it already has, and "where had the historical walk got
// to?", which is what stops a restart beginning that walk again from page one.
// Both were absent on 2026-08-11 and the first poll never finished.

// ---- a pool that can answer, not only record

type scriptedRow struct {
	values []any
	err    error
}

func (r *scriptedRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return assign(dest, r.values)
}

type scriptedRows struct {
	rows   [][]any
	cursor int
	err    error
}

func (r *scriptedRows) Next() bool { return r.cursor < len(r.rows) }
func (r *scriptedRows) Scan(dest ...any) error {
	row := r.rows[r.cursor]
	r.cursor++
	return assign(dest, row)
}
func (r *scriptedRows) Err() error { return r.err }
func (r *scriptedRows) Close()     {}

func assign(dest []any, values []any) error {
	for i, d := range dest {
		if i >= len(values) {
			break
		}
		switch ptr := d.(type) {
		case *int:
			ptr2, ok := values[i].(int)
			if !ok {
				return errors.New("type mismatch: int")
			}
			*ptr = ptr2
		case *int64:
			ptr2, ok := values[i].(int64)
			if !ok {
				return errors.New("type mismatch: int64")
			}
			*ptr = ptr2
		case *bool:
			ptr2, ok := values[i].(bool)
			if !ok {
				return errors.New("type mismatch: bool")
			}
			*ptr = ptr2
		case *string:
			ptr2, ok := values[i].(string)
			if !ok {
				return errors.New("type mismatch: string")
			}
			*ptr = ptr2
		case *time.Time:
			ptr2, ok := values[i].(time.Time)
			if !ok {
				return errors.New("type mismatch: time")
			}
			*ptr = ptr2
		}
	}
	return nil
}

type scriptedPool struct {
	rows     *scriptedRows
	row      *scriptedRow
	queryErr error
	execErr  error
	execs    int
}

func (p *scriptedPool) Exec(_ context.Context, _ string, _ ...any) (ghdb.CommandTag, error) {
	p.execs++
	if p.execErr != nil {
		return nil, p.execErr
	}
	return mockCommandTag{rowsAffected: 1}, nil
}

func (p *scriptedPool) Query(_ context.Context, _ string, _ ...any) (ghdb.Rows, error) {
	if p.queryErr != nil {
		return nil, p.queryErr
	}
	if p.rows == nil {
		return &scriptedRows{}, nil
	}
	return p.rows, nil
}

func (p *scriptedPool) QueryRow(_ context.Context, _ string, _ ...any) ghdb.Row {
	if p.row == nil {
		return &scriptedRow{}
	}
	return p.row
}

func newAdapter(pool ghdb.DBPool) *storeAdapter {
	return &storeAdapter{store: ghdb.NewStore(pool)}
}

// ---- the "already collected?" side

func TestStoreAdapter_SelectRunsForJobFetch(t *testing.T) {
	pool := &scriptedPool{rows: &scriptedRows{rows: [][]any{{int64(7)}, {int64(9)}}}}
	adapter := newAdapter(pool)

	got, err := adapter.SelectRunsForJobFetch(context.Background(), []ghpkg.RunJobSync{
		{RunID: 7, SyncKey: time.Now()},
		{RunID: 8, SyncKey: time.Now()},
		{RunID: 9, SyncKey: time.Now()},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != 7 || got[1] != 9 {
		t.Errorf("got %v, want [7 9]", got)
	}
}

func TestStoreAdapter_SelectRunsForJobFetchError(t *testing.T) {
	adapter := newAdapter(&scriptedPool{queryErr: errors.New("connection reset")})

	if _, err := adapter.SelectRunsForJobFetch(context.Background(),
		[]ghpkg.RunJobSync{{RunID: 1, SyncKey: time.Now()}}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestStoreAdapter_MarkJobsSynced(t *testing.T) {
	pool := &scriptedPool{}
	adapter := newAdapter(pool)

	if err := adapter.MarkJobsSynced(context.Background(), 1, time.Now()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool.execs != 1 {
		t.Errorf("exec calls = %d, want 1", pool.execs)
	}

	adapter = newAdapter(&scriptedPool{execErr: errors.New("deadlock")})
	if err := adapter.MarkJobsSynced(context.Background(), 1, time.Now()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestStoreAdapter_RunsMissingJobs(t *testing.T) {
	when := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	pool := &scriptedPool{rows: &scriptedRows{rows: [][]any{{int64(3), when}}}}
	adapter := newAdapter(pool)

	got, err := adapter.RunsMissingJobs(context.Background(), "repo", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].RunID != 3 || !got[0].SyncKey.Equal(when) {
		t.Errorf("got %+v, want run 3 at %v", got, when)
	}

	adapter = newAdapter(&scriptedPool{queryErr: errors.New("connection reset")})
	if _, err := adapter.RunsMissingJobs(context.Background(), "repo", 10); err == nil {
		t.Fatal("expected an error")
	}
}

// ---- the "where had we got to?" side

func TestStoreAdapter_ListRepositoryNames(t *testing.T) {
	pool := &scriptedPool{rows: &scriptedRows{rows: [][]any{{"alpha"}, {"zeta"}}}}
	adapter := newAdapter(pool)

	got, err := adapter.ListRepositoryNames(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "alpha" {
		t.Errorf("got %v, want [alpha zeta]", got)
	}

	adapter = newAdapter(&scriptedPool{queryErr: errors.New("connection reset")})
	if _, err := adapter.ListRepositoryNames(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestStoreAdapter_BackfillProgressRoundTrip(t *testing.T) {
	completed := time.Date(2026, 8, 11, 6, 0, 0, 0, time.UTC)
	pool := &scriptedPool{row: &scriptedRow{values: []any{
		4, true, completed, int64(3), int64(5),
	}}}
	adapter := newAdapter(pool)

	got, err := adapter.LoadBackfillProgress(context.Background(), "repo-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.NextPage != 4 || !got.RunsComplete || !got.CompletedAt.Equal(completed) ||
		got.PagesFetched != 3 || got.RequestsSpent != 5 {
		t.Errorf("progress = %+v, want the stored values", got)
	}

	if err := adapter.SaveBackfillProgress(context.Background(), ghpkg.BackfillProgress{
		Repo: "repo-a", NextPage: 5, CompletedAt: completed,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool.execs != 1 {
		t.Errorf("exec calls = %d, want 1", pool.execs)
	}
}

func TestStoreAdapter_BackfillProgressErrors(t *testing.T) {
	adapter := newAdapter(&scriptedPool{row: &scriptedRow{err: errors.New("connection reset")}})
	if _, err := adapter.LoadBackfillProgress(context.Background(), "repo-a"); err == nil {
		t.Fatal("expected a load error")
	}

	adapter = newAdapter(&scriptedPool{execErr: errors.New("deadlock")})
	if err := adapter.SaveBackfillProgress(context.Background(),
		ghpkg.BackfillProgress{Repo: "repo-a"}); err == nil {
		t.Fatal("expected a save error")
	}
}

func TestStoreAdapter_BackfillStats(t *testing.T) {
	adapter := newAdapter(&scriptedPool{row: &scriptedRow{values: []any{int64(12), int64(4)}}})

	got, err := adapter.BackfillStats(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.PendingJobRuns != 12 || got.ReposComplete != 4 {
		t.Errorf("stats = %+v, want 12 pending and 4 complete", got)
	}

	adapter = newAdapter(&scriptedPool{row: &scriptedRow{err: errors.New("connection reset")}})
	if _, err := adapter.BackfillStats(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

// The adapter must satisfy both halves of the split: the poller's writer and
// the backfiller's reader. If it ever stops doing so, the exporter loses either
// fresh data or its memory of what it has already collected.
func TestStoreAdapter_SatisfiesBothInterfaces(t *testing.T) {
	var _ ghpkg.StoreWriter = newAdapter(&scriptedPool{})
	var _ ghpkg.BackfillStore = newAdapter(&scriptedPool{})
}
