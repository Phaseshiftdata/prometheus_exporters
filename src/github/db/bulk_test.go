package db

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestUpsertRepositories(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	now := time.Now()
	repos := []Repository{
		{ID: 1, Name: "org/repo1", DefaultBranch: "main", Visibility: "public", UpdatedAt: &now},
		{ID: 2, Name: "org/repo2", DefaultBranch: "develop", Visibility: "private", Archived: true, UpdatedAt: &now},
	}

	if err := store.UpsertRepositories(ctx, repos); err != nil {
		t.Fatalf("UpsertRepositories() error: %v", err)
	}

	calls := pool.getExecCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 Exec calls, got %d", len(calls))
	}
}

func TestUpsertWorkflowRuns(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	runs := []WorkflowRun{
		{ID: 10, Repo: "org/r", Workflow: "ci.yml", CreatedAt: time.Now()},
		{ID: 11, Repo: "org/r", Workflow: "cd.yml", CreatedAt: time.Now()},
	}

	if err := store.UpsertWorkflowRuns(ctx, runs); err != nil {
		t.Fatalf("UpsertWorkflowRuns() error: %v", err)
	}

	calls := pool.getExecCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 Exec calls, got %d", len(calls))
	}
}

func TestUpsertWorkflowJobs(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	now := time.Now()
	jobs := []WorkflowJob{
		{ID: 20, RunID: 10, Name: "build", Conclusion: "success", StartedAt: &now, CompletedAt: &now},
		{ID: 21, RunID: 10, Name: "test", Conclusion: "success", StartedAt: &now, CompletedAt: &now},
	}

	if err := store.UpsertWorkflowJobs(ctx, jobs); err != nil {
		t.Fatalf("UpsertWorkflowJobs() error: %v", err)
	}

	calls := pool.getExecCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 Exec calls, got %d", len(calls))
	}
}

func TestUpsertPullRequests(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	prs := []PullRequest{
		{ID: 30, Repo: "org/r", Number: 1, State: "open", Author: "alice", CreatedAt: time.Now()},
		{ID: 31, Repo: "org/r", Number: 2, State: "closed", Author: "bob", CreatedAt: time.Now()},
	}

	if err := store.UpsertPullRequests(ctx, prs); err != nil {
		t.Fatalf("UpsertPullRequests() error: %v", err)
	}

	calls := pool.getExecCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 Exec calls, got %d", len(calls))
	}
}

func TestUpsertCommits(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	commits := []Commit{
		{SHA: "aaa", Repo: "org/r", Branch: "main", Author: "alice", Message: "first", CommittedAt: time.Now()},
		{SHA: "bbb", Repo: "org/r", Branch: "main", Author: "bob", Message: "second", CommittedAt: time.Now()},
	}

	if err := store.UpsertCommits(ctx, commits); err != nil {
		t.Fatalf("UpsertCommits() error: %v", err)
	}

	calls := pool.getExecCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 Exec calls, got %d", len(calls))
	}
}

func TestUpsertTags(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	now := time.Now()
	tags := []Tag{
		{Repo: "org/r", Name: "v1.0.0", TargetSHA: "aaa", CreatedAt: &now},
		{Repo: "org/r", Name: "v2.0.0", TargetSHA: "bbb", CreatedAt: &now},
	}

	if err := store.UpsertTags(ctx, tags); err != nil {
		t.Fatalf("UpsertTags() error: %v", err)
	}

	calls := pool.getExecCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 Exec calls, got %d", len(calls))
	}
}

func TestUpsertRepositoriesEmpty(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	if err := store.UpsertRepositories(ctx, nil); err != nil {
		t.Fatalf("UpsertRepositories(nil) error: %v", err)
	}
	if err := store.UpsertRepositories(ctx, []Repository{}); err != nil {
		t.Fatalf("UpsertRepositories([]) error: %v", err)
	}

	calls := pool.getExecCalls()
	if len(calls) != 0 {
		t.Fatalf("expected 0 Exec calls for empty slices, got %d", len(calls))
	}
}

func TestUpsertRepositoriesError(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	// Set error so the first Exec fails.
	pool.execErr = fmt.Errorf("db down")

	repos := []Repository{
		{ID: 1, Name: "org/repo1"},
		{ID: 2, Name: "org/repo2"},
	}

	err := store.UpsertRepositories(ctx, repos)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Should fail on the first item and stop.
	calls := pool.getExecCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 Exec call (fail on first), got %d", len(calls))
	}
}

func TestUpsertWorkflowRunsEmpty(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	if err := store.UpsertWorkflowRuns(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pool.getExecCalls()) != 0 {
		t.Fatal("expected 0 calls")
	}
}

func TestUpsertWorkflowJobsEmpty(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	if err := store.UpsertWorkflowJobs(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pool.getExecCalls()) != 0 {
		t.Fatal("expected 0 calls")
	}
}

func TestUpsertPullRequestsEmpty(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	if err := store.UpsertPullRequests(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pool.getExecCalls()) != 0 {
		t.Fatal("expected 0 calls")
	}
}

func TestUpsertCommitsEmpty(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	if err := store.UpsertCommits(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pool.getExecCalls()) != 0 {
		t.Fatal("expected 0 calls")
	}
}

func TestUpsertTagsEmpty(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	if err := store.UpsertTags(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pool.getExecCalls()) != 0 {
		t.Fatal("expected 0 calls")
	}
}

func TestUpsertWorkflowRunsError(t *testing.T) {
	pool := newMockPool()
	pool.execErr = fmt.Errorf("db down")
	store := NewStore(pool)
	err := store.UpsertWorkflowRuns(context.Background(), []WorkflowRun{{ID: 1, Repo: "r", Workflow: "w", CreatedAt: time.Now()}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUpsertWorkflowJobsError(t *testing.T) {
	pool := newMockPool()
	pool.execErr = fmt.Errorf("db down")
	store := NewStore(pool)
	err := store.UpsertWorkflowJobs(context.Background(), []WorkflowJob{{ID: 1, RunID: 1, Name: "j"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUpsertPullRequestsError(t *testing.T) {
	pool := newMockPool()
	pool.execErr = fmt.Errorf("db down")
	store := NewStore(pool)
	err := store.UpsertPullRequests(context.Background(), []PullRequest{{ID: 1, Repo: "r", Number: 1, State: "open", CreatedAt: time.Now()}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUpsertCommitsError(t *testing.T) {
	pool := newMockPool()
	pool.execErr = fmt.Errorf("db down")
	store := NewStore(pool)
	err := store.UpsertCommits(context.Background(), []Commit{{SHA: "a", Repo: "r", CommittedAt: time.Now()}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUpsertTagsError(t *testing.T) {
	pool := newMockPool()
	pool.execErr = fmt.Errorf("db down")
	store := NewStore(pool)
	err := store.UpsertTags(context.Background(), []Tag{{Repo: "r", Name: "v1"}})
	if err == nil {
		t.Fatal("expected error")
	}
}
