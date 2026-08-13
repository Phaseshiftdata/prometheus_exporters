package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// mockStore tracks all upsert calls for verification.
type mockStore struct {
	mu           sync.Mutex
	repos        []Repository
	workflowRuns []WorkflowRun
	workflowJobs []WorkflowJob
	pullRequests []PullRequest
	commits      []Commit
	tags         []Tag
	sampledDays  []time.Time

	repoErr        error
	workflowRunErr error
	workflowJobErr error
	prErr          error
	commitErr      error
	tagErr         error
	sampleErr      error

	// Job-fetch gating. The default is the cold-start answer -- nothing is
	// stored, so every run offered needs its jobs. selectedRuns overrides that
	// with a fixed answer, which is how a warm store is simulated.
	askedForJobFetch []RunJobSync
	selectedRuns     *[]int64
	selectErr        error
	syncedRuns       []RunJobSync
	markErr          error
}

func (s *mockStore) UpsertRepositories(_ context.Context, repos []Repository) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos = append(s.repos, repos...)
	return s.repoErr
}

func (s *mockStore) UpsertWorkflowRuns(_ context.Context, runs []WorkflowRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workflowRuns = append(s.workflowRuns, runs...)
	return s.workflowRunErr
}

func (s *mockStore) UpsertWorkflowJobs(_ context.Context, jobs []WorkflowJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workflowJobs = append(s.workflowJobs, jobs...)
	return s.workflowJobErr
}

func (s *mockStore) UpsertPullRequests(_ context.Context, prs []PullRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pullRequests = append(s.pullRequests, prs...)
	return s.prErr
}

func (s *mockStore) UpsertCommits(_ context.Context, commits []Commit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits = append(s.commits, commits...)
	return s.commitErr
}

func (s *mockStore) UpsertTags(_ context.Context, tags []Tag) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tags = append(s.tags, tags...)
	return s.tagErr
}

func (s *mockStore) SampleOpenPullRequests(_ context.Context, day time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sampledDays = append(s.sampledDays, day)
	return s.sampleErr
}

func (s *mockStore) SelectRunsForJobFetch(_ context.Context, candidates []RunJobSync) ([]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.askedForJobFetch = append(s.askedForJobFetch, candidates...)
	if s.selectErr != nil {
		return nil, s.selectErr
	}
	if s.selectedRuns != nil {
		return *s.selectedRuns, nil
	}
	ids := make([]int64, len(candidates))
	for i, c := range candidates {
		ids[i] = c.RunID
	}
	return ids, nil
}

func (s *mockStore) MarkJobsSynced(_ context.Context, runID int64, syncKey time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncedRuns = append(s.syncedRuns, RunJobSync{RunID: runID, SyncKey: syncKey})
	return s.markErr
}

func newTestPoller(t *testing.T, serverURL string) (*Client, *Poller) {
	t.Helper()
	auth := NewTestAuth("poll-token", time.Now().Add(1*time.Hour))
	client := NewClient(auth)

	// Create a simple HTTP client that follows the test server's TLS config.
	client.SetHTTPClient(createPlainClient(), serverURL)

	reg := prometheus.NewRegistry()
	metrics := NewMetrics(reg)
	poller := NewPoller(client, "test-org", 5*time.Minute, metrics)
	poller.nowFunc = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	return client, poller
}

func TestPoller_PollOnce(t *testing.T) {
	server := newTestGitHubServer(t)
	defer server.Close()

	_, poller := newTestPoller(t, server.URL)

	store := &mockStore{}
	poller.PollOnce(context.Background(), store)

	store.mu.Lock()
	defer store.mu.Unlock()

	if len(store.repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(store.repos))
	}
	if store.repos[0].Name != "test-repo" {
		t.Fatalf("expected test-repo, got %s", store.repos[0].Name)
	}
	if len(store.workflowRuns) != 1 {
		t.Fatalf("expected 1 workflow run, got %d", len(store.workflowRuns))
	}
	if len(store.workflowJobs) != 1 {
		t.Fatalf("expected 1 workflow job, got %d", len(store.workflowJobs))
	}
	if len(store.pullRequests) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(store.pullRequests))
	}
	if len(store.commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(store.commits))
	}
	if len(store.tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(store.tags))
	}
}

// The open pull request count is a level, not a total, so it has to be written
// once per day while the raw rows are still there. Yesterday is sampled as well
// as today because nothing guarantees a poll lands near midnight, and a day
// nobody sampled cannot be recovered after the prune.
func TestPoller_PollOnce_SamplesTodayAndYesterday(t *testing.T) {
	server := newTestGitHubServer(t)
	defer server.Close()

	_, poller := newTestPoller(t, server.URL)

	store := &mockStore{}
	poller.PollOnce(context.Background(), store)

	store.mu.Lock()
	defer store.mu.Unlock()

	if len(store.sampledDays) != 2 {
		t.Fatalf("expected 2 sampled days, got %d", len(store.sampledDays))
	}
	got := []string{
		store.sampledDays[0].Format("2006-01-02"),
		store.sampledDays[1].Format("2006-01-02"),
	}
	if got[0] != "2026-01-01" || got[1] != "2025-12-31" {
		t.Errorf("sampled %v, want [2026-01-01 2025-12-31]", got)
	}
}

// A failed sample costs one day of one trend line. Abandoning the poll would
// cost everything collected after it, so the loop continues.
func TestPoller_PollOnce_SampleErrorDoesNotStopThePoll(t *testing.T) {
	server := newTestGitHubServer(t)
	defer server.Close()

	_, poller := newTestPoller(t, server.URL)

	store := &mockStore{sampleErr: errors.New("connection reset")}
	poller.PollOnce(context.Background(), store)

	store.mu.Lock()
	defer store.mu.Unlock()

	if len(store.sampledDays) != 2 {
		t.Errorf("a failed sample stopped the second one: %d attempt(s)", len(store.sampledDays))
	}
	if len(store.repos) != 1 {
		t.Errorf("the poll did not complete: %d repo(s) collected", len(store.repos))
	}
}

func TestPoller_PollOnce_RepoError(t *testing.T) {
	server := newErrorServer(500)
	defer server.Close()

	_, poller := newTestPoller(t, server.URL)

	store := &mockStore{}
	poller.PollOnce(context.Background(), store)

	if len(store.repos) != 0 {
		t.Fatalf("expected 0 repos, got %d", len(store.repos))
	}
}

func TestPoller_PollOnce_StoreError(t *testing.T) {
	server := newTestGitHubServer(t)
	defer server.Close()

	_, poller := newTestPoller(t, server.URL)

	store := &mockStore{repoErr: errors.New("db error")}
	poller.PollOnce(context.Background(), store)

	// Should not crash; error is logged. No per-repo collection happens.
}

func TestPoller_RunContextCancel(t *testing.T) {
	server := newTestGitHubServer(t)
	defer server.Close()

	_, poller := newTestPoller(t, server.URL)
	poller.interval = 100 * time.Millisecond
	poller.nowFunc = time.Now

	store := &mockStore{}

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	err := poller.Run(ctx, store)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestPoller_Org(t *testing.T) {
	poller := &Poller{org: "my-org"}
	if poller.Org() != "my-org" {
		t.Fatalf("expected my-org, got %s", poller.Org())
	}
}

func TestFormatRepoFullName(t *testing.T) {
	got := formatRepoFullName("org", "repo")
	if got != "org/repo" {
		t.Fatalf("expected org/repo, got %s", got)
	}
}

func TestPoller_PollOnce_CollectorErrors(t *testing.T) {
	// Server that returns valid repos but errors for all other endpoints.
	mux := http.NewServeMux()
	mux.HandleFunc("/orgs/test-org/repos", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"id":             1,
				"name":           "fail-repo",
				"default_branch": "main",
				"visibility":     "private",
				"archived":       false,
				"updated_at":     "2026-01-01T00:00:00Z",
			},
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	_, poller := newTestPoller(t, server.URL)

	store := &mockStore{}
	poller.PollOnce(context.Background(), store)

	// Repos should be upserted successfully.
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(store.repos))
	}
	// All per-repo collectors should have errored, so no data.
	if len(store.workflowRuns) != 0 {
		t.Fatalf("expected 0 workflow runs, got %d", len(store.workflowRuns))
	}
	if len(store.pullRequests) != 0 {
		t.Fatalf("expected 0 PRs, got %d", len(store.pullRequests))
	}
	if len(store.commits) != 0 {
		t.Fatalf("expected 0 commits, got %d", len(store.commits))
	}
	if len(store.tags) != 0 {
		t.Fatalf("expected 0 tags, got %d", len(store.tags))
	}
}

func TestPoller_PollOnce_UpsertErrors(t *testing.T) {
	server := newTestGitHubServer(t)
	defer server.Close()

	_, poller := newTestPoller(t, server.URL)

	store := &mockStore{
		workflowRunErr: errors.New("run upsert error"),
		workflowJobErr: errors.New("job upsert error"),
		prErr:          errors.New("pr upsert error"),
		commitErr:      errors.New("commit upsert error"),
		tagErr:         errors.New("tag upsert error"),
	}
	// Should not crash even when all upserts fail.
	poller.PollOnce(context.Background(), store)
}

func TestPoller_ConvertFunctions(t *testing.T) {
	// Test convert with nil input.
	repos := convertRepos(nil)
	if len(repos) != 0 {
		t.Fatalf("expected 0, got %d", len(repos))
	}

	runs := convertWorkflowRuns(nil)
	if len(runs) != 0 {
		t.Fatalf("expected 0, got %d", len(runs))
	}

	jobs := convertWorkflowJobs(nil)
	if len(jobs) != 0 {
		t.Fatalf("expected 0, got %d", len(jobs))
	}

	prs := convertPullRequests(nil)
	if len(prs) != 0 {
		t.Fatalf("expected 0, got %d", len(prs))
	}

	commits := convertCommits(nil)
	if len(commits) != 0 {
		t.Fatalf("expected 0, got %d", len(commits))
	}

	tags := convertTags(nil)
	if len(tags) != 0 {
		t.Fatalf("expected 0, got %d", len(tags))
	}
}
