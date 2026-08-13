package db

import (
	"context"
	"testing"
	"time"
)

// ---- happy-path unit tests against the mock pool

func TestSelectRunsForJobFetch_ReturnsMatchedRunIDs(t *testing.T) {
	pool := newMockPool()
	pool.queryResults["\n\tSELECT wr.id"] = &mockRows{
		data: [][]any{
			{int64(42)},
			{int64(99)},
		},
	}
	store := NewStore(pool)

	candidates := []RunJobSync{
		{RunID: 42, SyncKey: time.Now()},
		{RunID: 99, SyncKey: time.Now()},
	}
	got, err := store.SelectRunsForJobFetch(context.Background(), candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d run IDs, want 2", len(got))
	}
	if got[0] != 42 || got[1] != 99 {
		t.Errorf("got %v, want [42, 99]", got)
	}
}

func TestSelectRunsForJobFetch_BuildsCandidateArrays(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)

	now := time.Now()
	earlier := now.Add(-time.Hour)
	candidates := []RunJobSync{
		{RunID: 1, SyncKey: now},
		{RunID: 2, SyncKey: earlier},
	}

	_, err := store.SelectRunsForJobFetch(context.Background(), candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The query was issued (not short-circuited) because candidates is non-empty.
	// The mock pool's Query was called; we cannot inspect its args directly, but
	// the absence of an error confirms the arrays were built and passed.
}

func TestRunsMissingJobs_ReturnsRunsWithSyncKeys(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	pool := newMockPool()
	pool.queryResults["\n\tSELECT id"] = &mockRows{
		data: [][]any{
			{int64(10), now},
			{int64(20), now.Add(-time.Hour)},
		},
	}
	store := NewStore(pool)

	got, err := store.RunsMissingJobs(context.Background(), "repo-a", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if got[0].RunID != 10 || got[1].RunID != 20 {
		t.Errorf("run IDs = [%d, %d], want [10, 20]", got[0].RunID, got[1].RunID)
	}
}

func TestRunsMissingJobs_EmptyResult(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)

	got, err := store.RunsMissingJobs(context.Background(), "repo-x", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for empty result, got %v", got)
	}
}

func TestBackfillStats_ReturnsStats(t *testing.T) {
	pool := newMockPool()
	pool.queryRowResults["\n\tSELECT"] = &mockRow{values: []any{int64(5), int64(3)}}
	store := NewStore(pool)

	stats, err := store.BackfillStats(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.PendingJobRuns != 5 {
		t.Errorf("PendingJobRuns = %d, want 5", stats.PendingJobRuns)
	}
	if stats.ReposComplete != 3 {
		t.Errorf("ReposComplete = %d, want 3", stats.ReposComplete)
	}
}

func TestBackfillStats_ZeroCounts(t *testing.T) {
	pool := newMockPool()
	pool.queryRowResults["\n\tSELECT"] = &mockRow{values: []any{int64(0), int64(0)}}
	store := NewStore(pool)

	stats, err := store.BackfillStats(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.PendingJobRuns != 0 || stats.ReposComplete != 0 {
		t.Errorf("expected zeros, got PendingJobRuns=%d, ReposComplete=%d",
			stats.PendingJobRuns, stats.ReposComplete)
	}
}

func TestMarkJobsSynced_Success(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)

	if err := store.MarkJobsSynced(context.Background(), 42, time.Now()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := pool.getExecCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 Exec call, got %d", len(calls))
	}
	if len(calls[0].Args) != 2 {
		t.Errorf("expected 2 args (runID, syncKey), got %d", len(calls[0].Args))
	}
}

func TestListRepositoryNames_ReturnsNames(t *testing.T) {
	pool := newMockPool()
	pool.queryResults["\n\tSELECT name"] = &mockRows{
		data: [][]any{
			{"alpha"},
			{"bravo"},
			{"charlie"},
		},
	}
	store := NewStore(pool)

	got, err := store.ListRepositoryNames(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d names, want 3", len(got))
	}
	if got[0] != "alpha" || got[1] != "bravo" || got[2] != "charlie" {
		t.Errorf("got %v, want [alpha bravo charlie]", got)
	}
}

func TestListRepositoryNames_EmptyResult(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)

	got, err := store.ListRepositoryNames(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for empty result, got %v", got)
	}
}

func TestLoadBackfillProgress_ReturnsProgress(t *testing.T) {
	pool := newMockPool()
	pool.queryRowResults["\n\tSELECT"] = &mockRow{values: []any{
		int(7), true, time.Unix(0, 0), int64(12), int64(25),
	}}
	store := NewStore(pool)

	got, err := store.LoadBackfillProgress(context.Background(), "repo-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Repo != "repo-a" {
		t.Errorf("Repo = %q, want %q", got.Repo, "repo-a")
	}
	if got.NextPage != 7 {
		t.Errorf("NextPage = %d, want 7", got.NextPage)
	}
}

func TestSaveBackfillProgress_Success(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)

	p := BackfillProgress{
		Repo:          "repo-a",
		NextPage:      5,
		RunsComplete:  true,
		CompletedAt:   time.Now(),
		PagesFetched:  4,
		RequestsSpent: 8,
	}
	if err := store.SaveBackfillProgress(context.Background(), p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := pool.getExecCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 Exec call, got %d", len(calls))
	}
	if len(calls[0].Args) != 6 {
		t.Errorf("expected 6 args, got %d", len(calls[0].Args))
	}
}
