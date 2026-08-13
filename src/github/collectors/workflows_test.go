package collectors

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// recordingClient is an APIClient that answers from a canned map and remembers
// every URL it was asked for. The URLs are the point of most of these tests:
// what matters after 2026-08-11 is not only what the collector returns but how
// many requests it took and what it asked for.
type recordingClient struct {
	urls     []string
	payloads []any
	errs     []error
	modified []bool
	call     int
}

func (c *recordingClient) Get(_ context.Context, url string, result any) (bool, error) {
	c.urls = append(c.urls, url)
	i := c.call
	c.call++
	if i < len(c.errs) && c.errs[i] != nil {
		return false, c.errs[i]
	}
	modified := true
	if i < len(c.modified) {
		modified = c.modified[i]
	}
	if !modified {
		return false, nil
	}
	if i < len(c.payloads) && c.payloads[i] != nil {
		raw, err := json.Marshal(c.payloads[i])
		if err != nil {
			return false, err
		}
		if err := json.Unmarshal(raw, result); err != nil {
			return false, err
		}
	}
	return true, nil
}

func runJSON(id int64, created string) map[string]any {
	return map[string]any{
		"id":             id,
		"name":           "CI",
		"head_branch":    "main",
		"conclusion":     "success",
		"run_attempt":    1,
		"created_at":     created,
		"run_started_at": created,
		"updated_at":     created,
	}
}

// One page is one request. That is the property the whole redesign rests on:
// the old Collect() looped over pages and then issued a jobs request per run,
// so a single call could be thousands of requests.
func TestCollectRunsPage_IsExactlyOneRequest(t *testing.T) {
	client := &recordingClient{payloads: []any{
		map[string]any{"workflow_runs": []map[string]any{
			runJSON(10, "2026-01-01T00:00:00Z"),
		}},
	}}
	collector := &WorkflowCollector{Client: client}

	page, err := collector.CollectRunsPage(context.Background(), "org", "repo", RunQuery{Page: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(client.urls) != 1 {
		t.Fatalf("expected exactly 1 request, got %d: %v", len(client.urls), client.urls)
	}
	if len(page.Runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(page.Runs))
	}
	if page.More {
		t.Error("a short page should not claim more pages exist")
	}
	run := page.Runs[0]
	if run.Workflow != "CI" || run.Repo != "repo" || run.Conclusion != "success" {
		t.Errorf("unexpected run: %+v", run)
	}
	if run.RunStartedAt == nil || run.UpdatedAt == nil {
		t.Error("expected run_started_at and updated_at to be parsed")
	}
}

// The horizon must reach GitHub, because filtering at the far end is what makes
// the request cheap rather than merely correct.
func TestCollectRunsPage_SendsTheHorizonToGitHub(t *testing.T) {
	client := &recordingClient{payloads: []any{map[string]any{"workflow_runs": []map[string]any{}}}}
	collector := &WorkflowCollector{Client: client}

	since := time.Date(2026, 5, 14, 9, 30, 0, 0, time.UTC)
	if _, err := collector.CollectRunsPage(
		context.Background(), "org", "repo", RunQuery{Page: 3, CreatedSince: since},
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := client.urls[0]
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("collector built an unparseable URL %q: %v", got, err)
	}
	q := parsed.Query()
	if q.Get("created") != ">=2026-05-14" {
		t.Errorf("created = %q, want %q (URL %s)", q.Get("created"), ">=2026-05-14", got)
	}
	if q.Get("page") != "3" {
		t.Errorf("page = %q, want 3", q.Get("page"))
	}
	if q.Get("per_page") != "100" {
		t.Errorf("per_page = %q, want 100", q.Get("per_page"))
	}
}

// Belt and braces. If GitHub's own filter lets an old run through, the local
// filter drops it -- because offering a run from beyond the retention window to
// the accumulating rollups counts its day twice.
func TestCollectRunsPage_DropsRunsBeyondTheHorizonEvenIfGitHubDoesNot(t *testing.T) {
	client := &recordingClient{payloads: []any{
		map[string]any{"workflow_runs": []map[string]any{
			runJSON(1, "2026-06-01T00:00:00Z"),
			runJSON(2, "2025-01-01T00:00:00Z"),
		}},
	}}
	collector := &WorkflowCollector{Client: client}

	since := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	page, err := collector.CollectRunsPage(
		context.Background(), "org", "repo", RunQuery{Page: 1, CreatedSince: since})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Runs) != 1 || page.Runs[0].ID != 1 {
		t.Fatalf("expected only the in-horizon run, got %+v", page.Runs)
	}
	if page.DroppedBeyondHorizon != 1 {
		t.Errorf("DroppedBeyondHorizon = %d, want 1", page.DroppedBeyondHorizon)
	}
}

// More is judged from the size of the page GitHub returned, not from what
// survived the horizon filter: a page can be full of runs that are all too old,
// and stopping there would be stopping on the wrong evidence.
func TestCollectRunsPage_MoreCountsTheRawPage(t *testing.T) {
	full := make([]map[string]any, RunsPerPage)
	for i := range full {
		full[i] = runJSON(int64(1000+i), "2020-01-01T00:00:00Z")
	}
	client := &recordingClient{payloads: []any{map[string]any{"workflow_runs": full}}}
	collector := &WorkflowCollector{Client: client}

	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	page, err := collector.CollectRunsPage(
		context.Background(), "org", "repo", RunQuery{Page: 1, CreatedSince: since})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Runs) != 0 {
		t.Fatalf("expected every run filtered out, got %d", len(page.Runs))
	}
	if !page.More {
		t.Error("a full page must report More even when nothing survived the horizon")
	}
	if page.DroppedBeyondHorizon != RunsPerPage {
		t.Errorf("DroppedBeyondHorizon = %d, want %d", page.DroppedBeyondHorizon, RunsPerPage)
	}
}

func TestCollectRunsPage_NotModified(t *testing.T) {
	client := &recordingClient{modified: []bool{false}}
	collector := &WorkflowCollector{Client: client}

	page, err := collector.CollectRunsPage(context.Background(), "org", "repo", RunQuery{Page: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !page.NotModified {
		t.Error("expected NotModified")
	}
	if len(page.Runs) != 0 || page.More {
		t.Errorf("a 304 carries no runs and no claim about further pages: %+v", page)
	}
}

func TestCollectRunsPage_PageNumberIsNormalised(t *testing.T) {
	client := &recordingClient{payloads: []any{map[string]any{"workflow_runs": []map[string]any{}}}}
	collector := &WorkflowCollector{Client: client}

	if _, err := collector.CollectRunsPage(
		context.Background(), "org", "repo", RunQuery{Page: 0},
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(client.urls[0], "page=1") {
		t.Errorf("page 0 should be asked for as page 1, got %s", client.urls[0])
	}
}

func TestCollectRunsPage_Error(t *testing.T) {
	client := &recordingClient{errs: []error{errors.New("boom")}}
	collector := &WorkflowCollector{Client: client}

	_, err := collector.CollectRunsPage(context.Background(), "org", "repo", RunQuery{Page: 2})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "page 2") || !strings.Contains(err.Error(), "repo") {
		t.Errorf("error should name the page and repository: %v", err)
	}
}

func TestCollectJobs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/repo/actions/runs/10/jobs", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jobs": []map[string]any{
				{
					"id":           20,
					"run_id":       10,
					"name":         "build",
					"conclusion":   "success",
					"started_at":   "2026-01-01T00:00:05Z",
					"completed_at": "2026-01-01T00:04:00Z",
				},
				{
					"id":         21,
					"run_id":     10,
					"name":       "queued-forever",
					"conclusion": "",
				},
			},
		})
	})

	collector := &WorkflowCollector{Client: newHTTPTestClient(t, mux)}
	jobs, err := collector.CollectJobs(context.Background(), "org", "repo", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	if jobs[0].Name != "build" || jobs[0].RunID != 10 {
		t.Errorf("unexpected job: %+v", jobs[0])
	}
	if jobs[0].StartedAt == nil || jobs[0].CompletedAt == nil {
		t.Error("expected both timestamps parsed on the finished job")
	}
	if jobs[1].StartedAt != nil || jobs[1].CompletedAt != nil {
		t.Error("a job with no timestamps should carry nil, not the zero time")
	}
}

func TestCollectJobs_Error(t *testing.T) {
	client := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	collector := &WorkflowCollector{Client: client}

	_, err := collector.CollectJobs(context.Background(), "org", "repo", 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "jobs for run 10") {
		t.Errorf("error should name the run: %v", err)
	}
}

func TestCollectJobs_EmptyIsNotNil(t *testing.T) {
	client := &recordingClient{payloads: []any{map[string]any{"jobs": []map[string]any{}}}}
	collector := &WorkflowCollector{Client: client}

	jobs, err := collector.CollectJobs(context.Background(), "org", "repo", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if jobs == nil || len(jobs) != 0 {
		t.Errorf("expected an empty slice, got %v", jobs)
	}
}
