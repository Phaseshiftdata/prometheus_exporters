package db

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewStore(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	if store == nil {
		t.Fatal("NewStore returned nil")
	}
	if store.Pool() != pool {
		t.Error("Pool() should return the injected pool")
	}
}

func TestStoreClose(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	// Close is a no-op; just verify it doesn't panic.
	store.Close()
}

func TestUpsertRepository(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	now := time.Now()
	repo := Repository{
		ID:            42,
		Name:          "org/repo",
		DefaultBranch: "main",
		Visibility:    "private",
		Archived:      false,
		UpdatedAt:     &now,
	}

	if err := store.UpsertRepository(ctx, repo); err != nil {
		t.Fatalf("UpsertRepository() error: %v", err)
	}

	calls := pool.getExecCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 Exec call, got %d", len(calls))
	}

	call := calls[0]
	if !strings.Contains(call.SQL, "INSERT INTO repositories") {
		t.Error("SQL should contain INSERT INTO repositories")
	}
	if !strings.Contains(call.SQL, "ON CONFLICT") {
		t.Error("SQL should contain ON CONFLICT for upsert")
	}
	if len(call.Args) != 6 {
		t.Errorf("expected 6 args, got %d", len(call.Args))
	}
}

func TestUpsertRepository_Idempotent(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	repo := Repository{ID: 1, Name: "org/repo", DefaultBranch: "main"}

	// Upsert twice.
	if err := store.UpsertRepository(ctx, repo); err != nil {
		t.Fatalf("first UpsertRepository() error: %v", err)
	}
	if err := store.UpsertRepository(ctx, repo); err != nil {
		t.Fatalf("second UpsertRepository() error: %v", err)
	}

	calls := pool.getExecCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 Exec calls, got %d", len(calls))
	}
	// Both calls should have identical SQL.
	if calls[0].SQL != calls[1].SQL {
		t.Error("idempotent upserts should produce identical SQL")
	}
}

func TestUpsertWorkflowRun(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	run := WorkflowRun{
		ID:        100,
		Repo:      "org/repo",
		Workflow:  "ci.yml",
		Branch:    "main",
		Conclusion: "success",
		Attempt:   1,
		CreatedAt: time.Now(),
	}

	if err := store.UpsertWorkflowRun(ctx, run); err != nil {
		t.Fatalf("UpsertWorkflowRun() error: %v", err)
	}

	calls := pool.getExecCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if !strings.Contains(calls[0].SQL, "INSERT INTO workflow_runs") {
		t.Error("SQL should contain INSERT INTO workflow_runs")
	}
	if !strings.Contains(calls[0].SQL, "ON CONFLICT") {
		t.Error("SQL should contain ON CONFLICT")
	}
	if len(calls[0].Args) != 9 {
		t.Errorf("expected 9 args, got %d", len(calls[0].Args))
	}
}

func TestUpsertWorkflowRun_Idempotent(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	run := WorkflowRun{ID: 1, Repo: "r", Workflow: "w", CreatedAt: time.Now()}
	_ = store.UpsertWorkflowRun(ctx, run)
	_ = store.UpsertWorkflowRun(ctx, run)

	calls := pool.getExecCalls()
	if calls[0].SQL != calls[1].SQL {
		t.Error("idempotent upserts should produce identical SQL")
	}
}

func TestUpsertWorkflowJob(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	now := time.Now()
	job := WorkflowJob{
		ID:          200,
		RunID:       100,
		Name:        "build",
		Conclusion:  "success",
		StartedAt:   &now,
		CompletedAt: &now,
	}

	if err := store.UpsertWorkflowJob(ctx, job); err != nil {
		t.Fatalf("UpsertWorkflowJob() error: %v", err)
	}

	calls := pool.getExecCalls()
	if !strings.Contains(calls[0].SQL, "INSERT INTO workflow_jobs") {
		t.Error("SQL should contain INSERT INTO workflow_jobs")
	}
	if len(calls[0].Args) != 6 {
		t.Errorf("expected 6 args, got %d", len(calls[0].Args))
	}
}

func TestUpsertPullRequest(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	pr := PullRequest{
		ID:        300,
		Repo:      "org/repo",
		Number:    42,
		State:     "open",
		Author:    "user",
		CreatedAt: time.Now(),
	}

	if err := store.UpsertPullRequest(ctx, pr); err != nil {
		t.Fatalf("UpsertPullRequest() error: %v", err)
	}

	calls := pool.getExecCalls()
	if !strings.Contains(calls[0].SQL, "INSERT INTO pull_requests") {
		t.Error("SQL should contain INSERT INTO pull_requests")
	}
	if !strings.Contains(calls[0].SQL, "ON CONFLICT") {
		t.Error("SQL should contain ON CONFLICT")
	}
	if len(calls[0].Args) != 9 {
		t.Errorf("expected 9 args, got %d", len(calls[0].Args))
	}
}

func TestUpsertPullRequest_Idempotent(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	pr := PullRequest{ID: 1, Repo: "r", Number: 1, State: "open", CreatedAt: time.Now()}
	_ = store.UpsertPullRequest(ctx, pr)
	_ = store.UpsertPullRequest(ctx, pr)

	calls := pool.getExecCalls()
	if calls[0].SQL != calls[1].SQL {
		t.Error("idempotent upserts should produce identical SQL")
	}
}

func TestUpsertCommit(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	commit := Commit{
		SHA:         "abc123",
		Repo:        "org/repo",
		Branch:      "main",
		Author:      "user",
		Message:     "fix bug",
		CommittedAt: time.Now(),
	}

	if err := store.UpsertCommit(ctx, commit); err != nil {
		t.Fatalf("UpsertCommit() error: %v", err)
	}

	calls := pool.getExecCalls()
	if !strings.Contains(calls[0].SQL, "INSERT INTO commits") {
		t.Error("SQL should contain INSERT INTO commits")
	}
	if len(calls[0].Args) != 6 {
		t.Errorf("expected 6 args, got %d", len(calls[0].Args))
	}
}

func TestUpsertCommit_Idempotent(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	c := Commit{SHA: "abc", Repo: "r", CommittedAt: time.Now()}
	_ = store.UpsertCommit(ctx, c)
	_ = store.UpsertCommit(ctx, c)

	calls := pool.getExecCalls()
	if calls[0].SQL != calls[1].SQL {
		t.Error("idempotent upserts should produce identical SQL")
	}
}

func TestUpsertTag(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	now := time.Now()
	tag := Tag{
		Repo:      "org/repo",
		Name:      "v1.0.0",
		TargetSHA: "abc123",
		CreatedAt: &now,
	}

	if err := store.UpsertTag(ctx, tag); err != nil {
		t.Fatalf("UpsertTag() error: %v", err)
	}

	calls := pool.getExecCalls()
	if !strings.Contains(calls[0].SQL, "INSERT INTO tags") {
		t.Error("SQL should contain INSERT INTO tags")
	}
	if !strings.Contains(calls[0].SQL, "ON CONFLICT (repo, name)") {
		t.Error("SQL should conflict on (repo, name)")
	}
	if len(calls[0].Args) != 4 {
		t.Errorf("expected 4 args, got %d", len(calls[0].Args))
	}
}

func TestUpsertTag_Idempotent(t *testing.T) {
	pool := newMockPool()
	store := NewStore(pool)
	ctx := context.Background()

	tag := Tag{Repo: "r", Name: "v1"}
	_ = store.UpsertTag(ctx, tag)
	_ = store.UpsertTag(ctx, tag)

	calls := pool.getExecCalls()
	if calls[0].SQL != calls[1].SQL {
		t.Error("idempotent upserts should produce identical SQL")
	}
}

func TestUpsertQueries_AllContainOnConflict(t *testing.T) {
	for name, sql := range UpsertQueries {
		if !strings.Contains(sql, "ON CONFLICT") {
			t.Errorf("UpsertQueries[%q] missing ON CONFLICT clause", name)
		}
		if !strings.Contains(sql, "INSERT INTO") {
			t.Errorf("UpsertQueries[%q] missing INSERT INTO", name)
		}
		if !strings.Contains(sql, "DO UPDATE SET") {
			t.Errorf("UpsertQueries[%q] missing DO UPDATE SET", name)
		}
	}
}

func TestUpsertQueries_ParameterCounts(t *testing.T) {
	expected := map[string]int{
		"repository":   6,
		"workflow_run":  9,
		"workflow_job":  6,
		"pull_request":  9,
		"commit":        6,
		"tag":           4,
	}

	for name, sql := range UpsertQueries {
		want, ok := expected[name]
		if !ok {
			t.Errorf("unexpected query name %q", name)
			continue
		}
		// Count $N placeholders.
		count := 0
		for i := 1; i <= 20; i++ {
			placeholder := "$" + strings.Repeat("", 0) + itoa(i)
			if strings.Contains(sql, placeholder) {
				count = i
			}
		}
		if count != want {
			t.Errorf("UpsertQueries[%q]: expected %d parameters, found up to $%d", name, want, count)
		}
	}
}

// itoa is a simple int-to-string for test use.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	result := ""
	for n > 0 {
		result = string(rune('0'+n%10)) + result
		n /= 10
	}
	return result
}

func TestUpsertError_WrapsContext(t *testing.T) {
	pool := newMockPool()
	pool.execErr = context.DeadlineExceeded
	store := NewStore(pool)
	ctx := context.Background()

	tests := []struct {
		name string
		fn   func() error
	}{
		{"repository", func() error { return store.UpsertRepository(ctx, Repository{ID: 1, Name: "r"}) }},
		{"workflow_run", func() error { return store.UpsertWorkflowRun(ctx, WorkflowRun{ID: 1, Repo: "r", Workflow: "w", CreatedAt: time.Now()}) }},
		{"workflow_job", func() error { return store.UpsertWorkflowJob(ctx, WorkflowJob{ID: 1, RunID: 1, Name: "j"}) }},
		{"pull_request", func() error { return store.UpsertPullRequest(ctx, PullRequest{ID: 1, Repo: "r", Number: 1, State: "open", CreatedAt: time.Now()}) }},
		{"commit", func() error { return store.UpsertCommit(ctx, Commit{SHA: "a", Repo: "r", CommittedAt: time.Now()}) }},
		{"tag", func() error { return store.UpsertTag(ctx, Tag{Repo: "r", Name: "v1"}) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "upsert") {
				t.Errorf("error should contain 'upsert', got: %v", err)
			}
		})
	}
}
