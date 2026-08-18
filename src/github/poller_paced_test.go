package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// These tests are about arithmetic, not features: how many requests a poll
// issues, and for what.
//
// On 2026-08-11 the answer was "thousands, back to back, mostly for data
// already in PostgreSQL", and GitHub's secondary rate limiter answered 403 with
// the primary quota at 5247 of 5250. Every test below fails if the poll starts
// walking history again or re-fetching jobs it has already got, which is the
// only kind of regression test that would have caught the incident.

// countingGitHub is a GitHub stand-in that remembers what was asked of it.
type countingGitHub struct {
	*httptest.Server

	mu            sync.Mutex
	runsRequests  int
	lastRunsQuery url.Values
	jobsRequests  []int64

	// runIDs are the runs the runs endpoint reports, newest first.
	runIDs []int64
	// runsStatus, when non-zero, is returned instead of a page of runs.
	runsStatus int
	// jobsStatus, when non-zero, is returned instead of a page of jobs.
	jobsStatus int
	// etag, when set, makes the runs endpoint answer 304 to a conditional
	// request -- the real behavior the client's ETag cache relies on.
	etag string
}

func newCountingGitHub(t *testing.T, g *countingGitHub) *countingGitHub {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/orgs/test-org/repos", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": 1, "name": "test-repo", "default_branch": "main",
			"visibility": "private", "archived": false,
			"updated_at": "2026-01-01T00:00:00Z",
		}})
	})

	mux.HandleFunc("/repos/test-org/test-repo/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.runsRequests++
		g.lastRunsQuery = r.URL.Query()
		status, etag, ids := g.runsStatus, g.etag, append([]int64(nil), g.runIDs...)
		g.mu.Unlock()

		if status != 0 {
			w.WriteHeader(status)
			return
		}
		if etag != "" {
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", etag)
		}

		runs := make([]map[string]any, 0, len(ids))
		for _, id := range ids {
			runs = append(runs, map[string]any{
				"id": id, "name": "CI", "head_branch": "main",
				"conclusion": "success", "run_attempt": 1,
				"created_at":     "2026-01-01T00:00:00Z",
				"run_started_at": "2026-01-01T00:00:01Z",
				"updated_at":     "2026-01-01T00:01:00Z",
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": runs})
	})

	mux.HandleFunc("/repos/test-org/test-repo/actions/runs/", func(w http.ResponseWriter, r *http.Request) {
		// .../actions/runs/<id>/jobs
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		id, _ := strconv.ParseInt(parts[len(parts)-2], 10, 64)

		g.mu.Lock()
		g.jobsRequests = append(g.jobsRequests, id)
		status := g.jobsStatus
		g.mu.Unlock()

		if status != 0 {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jobs": []map[string]any{{
			"id": id * 10, "run_id": id, "name": "build", "conclusion": "success",
			"started_at": "2026-01-01T00:00:01Z", "completed_at": "2026-01-01T00:01:00Z",
		}}})
	})

	// Everything else answers empty rather than 404 so that the noise of other
	// collectors failing does not obscure what these tests measure.
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})

	g.Server = httptest.NewServer(mux)
	t.Cleanup(g.Server.Close)
	return g
}

func (g *countingGitHub) counts() (runs int, jobs []int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.runsRequests, append([]int64(nil), g.jobsRequests...)
}

// ---- metric readers, so tests can assert on what the exporter would publish

func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("reading counter: %v", err)
	}
	return m.GetCounter().GetValue()
}

func counterVecValue(t *testing.T, v *prometheus.CounterVec, labels ...string) float64 {
	t.Helper()
	c, err := v.GetMetricWithLabelValues(labels...)
	if err != nil {
		t.Fatalf("reading counter %v: %v", labels, err)
	}
	return counterValue(t, c)
}

func gaugeValue(t *testing.T, g prometheus.Gauge) float64 {
	t.Helper()
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		t.Fatalf("reading gauge: %v", err)
	}
	return m.GetGauge().GetValue()
}

// A poll fetches ONE page of runs per repository. The old collector paged until
// the pages ran out, and with 163 runs in one repository here that alone was
// two requests before a single job was fetched -- and thousands after.
func TestPoller_FetchesOneRunsPagePerRepo(t *testing.T) {
	ids := make([]int64, 100) // a full page: the old code would have asked for page 2
	for i := range ids {
		ids[i] = int64(1000 + i)
	}
	gh := newCountingGitHub(t, &countingGitHub{runIDs: ids})

	_, poller := newTestPoller(t, gh.URL)
	poller.PollOnce(context.Background(), &mockStore{})

	runs, _ := gh.counts()
	if runs != 1 {
		t.Fatalf("poll issued %d runs requests, want exactly 1", runs)
	}
}

// The saving that makes a warm start cheap: a run whose updated_at has not
// moved is not asked about at all.
func TestPoller_SkipsJobsForRunsAlreadyStored(t *testing.T) {
	gh := newCountingGitHub(t, &countingGitHub{runIDs: []int64{100, 101, 102}})

	_, poller := newTestPoller(t, gh.URL)
	none := []int64{}
	store := &mockStore{selectedRuns: &none}
	poller.PollOnce(context.Background(), store)

	_, jobs := gh.counts()
	if len(jobs) != 0 {
		t.Fatalf("jobs were fetched for runs already stored: %v", jobs)
	}
	if got := counterValue(t, poller.metrics.JobFetchesSkippedTotal); got != 3 {
		t.Errorf("job_fetches_skipped_total = %v, want 3", got)
	}
	if len(store.askedForJobFetch) != 3 {
		t.Errorf("store was asked about %d runs, want 3", len(store.askedForJobFetch))
	}
	// The question asked of the store must carry the run's updated_at, since
	// that is the only thing that can tell a changed run from an unchanged one.
	want := time.Date(2026, 1, 1, 0, 1, 0, 0, time.UTC)
	if !store.askedForJobFetch[0].SyncKey.Equal(want) {
		t.Errorf("sync key = %v, want the run's updated_at %v",
			store.askedForJobFetch[0].SyncKey, want)
	}
}

// The budget is for the whole cycle, so the cost of a poll does not grow with
// the number of repositories -- which is exactly how 25 repositories turned a
// per-repository cost into an outage.
func TestPoller_JobBudgetBoundsTheCycleAndDefersTheRest(t *testing.T) {
	gh := newCountingGitHub(t, &countingGitHub{runIDs: []int64{100, 101, 102, 103, 104}})

	_, poller := newTestPoller(t, gh.URL)
	WithJobBudget(2)(poller)

	store := &mockStore{}
	poller.PollOnce(context.Background(), store)

	_, jobs := gh.counts()
	if len(jobs) != 2 {
		t.Fatalf("poll issued %d jobs requests, want 2 (the budget)", len(jobs))
	}
	if got := counterValue(t, poller.metrics.JobBudgetExhaustedTotal); got != 1 {
		t.Errorf("job_budget_exhausted_total = %v, want 1", got)
	}
	// Deferred, not dropped: the runs are stored, so the backfiller will find
	// them by asking the database what is still missing.
	if len(store.workflowRuns) != 5 {
		t.Errorf("stored %d runs, want all 5", len(store.workflowRuns))
	}
	if len(store.syncedRuns) != 2 {
		t.Errorf("marked %d runs synced, want 2", len(store.syncedRuns))
	}
}

// The budget is announced once per cycle, not once per repository that hits it.
func TestJobBudget_ReportsExhaustionOnce(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	b := &jobBudget{remaining: 1, metrics: m}

	if !b.take() {
		t.Fatal("first take should succeed")
	}
	for range 5 {
		if b.take() {
			t.Fatal("take should fail once the budget is gone")
		}
	}
	if got := counterValue(t, m.JobBudgetExhaustedTotal); got != 1 {
		t.Errorf("job_budget_exhausted_total = %v, want 1", got)
	}
}

// Runs from beyond the retention window must never be asked for: they cannot
// be stored, and offering one to the accumulating rollups counts its day twice.
func TestPoller_AsksGitHubToWithholdRunsBeyondTheHorizon(t *testing.T) {
	gh := newCountingGitHub(t, &countingGitHub{runIDs: []int64{100}})

	_, poller := newTestPoller(t, gh.URL) // now is fixed at 2026-01-01
	poller.PollOnce(context.Background(), &mockStore{})

	gh.mu.Lock()
	created := gh.lastRunsQuery.Get("created")
	gh.mu.Unlock()

	want := ">=" + time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).
		Add(-CollectionHorizon).Format("2006-01-02")
	if created != want {
		t.Errorf("created = %q, want %q", created, want)
	}
	if CollectionHorizon >= RetentionWindow {
		t.Fatalf("the horizon must sit inside the retention window: %v vs %v",
			CollectionHorizon, RetentionWindow)
	}
}

// A 304 on the runs page means nothing on it has changed, so there is nothing
// to store and no job that could have moved.
func TestPoller_NotModifiedRunsPageCostsNothingFurther(t *testing.T) {
	gh := newCountingGitHub(t, &countingGitHub{runIDs: []int64{100}, etag: `"abc"`})

	_, poller := newTestPoller(t, gh.URL)
	store := &mockStore{}

	poller.PollOnce(context.Background(), store) // primes the ETag
	first := len(store.workflowRuns)
	poller.PollOnce(context.Background(), store) // 304

	if len(store.workflowRuns) != first {
		t.Errorf("a 304 stored more runs: %d then %d", first, len(store.workflowRuns))
	}
	_, jobs := gh.counts()
	if len(jobs) != 1 {
		t.Errorf("jobs requests = %d, want 1 (the second poll should have asked for none)", len(jobs))
	}
	if got := gaugeValue(t, poller.metrics.LastSuccessTimestamp.WithLabelValues("workflows")); got == 0 {
		t.Error("a 304 is a success and should update last_success_timestamp")
	}
}

// A failed jobs request is very likely a 403 from the secondary limiter. The
// one thing that must not happen next is another request.
func TestPoller_JobFailureStopsFurtherJobRequestsForThatRepo(t *testing.T) {
	gh := newCountingGitHub(t, &countingGitHub{
		runIDs:     []int64{100, 101, 102},
		jobsStatus: http.StatusForbidden,
	})

	_, poller := newTestPoller(t, gh.URL)
	poller.PollOnce(context.Background(), &mockStore{})

	_, jobs := gh.counts()
	if len(jobs) != 1 {
		t.Fatalf("kept asking after a failure: %d jobs requests", len(jobs))
	}
	if got := counterVecValue(t, poller.metrics.ScrapeErrorsTotal, "workflows"); got < 1 {
		t.Errorf("scrape_errors_total{workflows} = %v, want at least 1", got)
	}
}

func TestPoller_RequestsAreCounted(t *testing.T) {
	gh := newCountingGitHub(t, &countingGitHub{runIDs: []int64{100, 101}})

	_, poller := newTestPoller(t, gh.URL)
	poller.PollOnce(context.Background(), &mockStore{})

	if got := counterVecValue(t, poller.metrics.APIRequestsTotal, "poll", "runs_page"); got != 1 {
		t.Errorf("api_requests_total{poll,runs_page} = %v, want 1", got)
	}
	if got := counterVecValue(t, poller.metrics.APIRequestsTotal, "poll", "jobs"); got != 2 {
		t.Errorf("api_requests_total{poll,jobs} = %v, want 2", got)
	}
}

func TestPoller_StoreFailuresAreCountedAndDoNotPanic(t *testing.T) {
	cases := []struct {
		name  string
		store *mockStore
	}{
		{"runs upsert", &mockStore{workflowRunErr: fmt.Errorf("runs upsert failed")}},
		{"select", &mockStore{selectErr: fmt.Errorf("select failed")}},
		{"jobs upsert", &mockStore{workflowJobErr: fmt.Errorf("jobs upsert failed")}},
		{"mark synced", &mockStore{markErr: fmt.Errorf("mark failed")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gh := newCountingGitHub(t, &countingGitHub{runIDs: []int64{100, 101}})
			_, poller := newTestPoller(t, gh.URL)
			poller.PollOnce(context.Background(), tc.store)

			if got := counterVecValue(t, poller.metrics.ScrapeErrorsTotal, "workflows"); got < 1 {
				t.Errorf("scrape_errors_total{workflows} = %v, want at least 1", got)
			}
		})
	}
}

func TestPoller_RunsPageFailureIsCounted(t *testing.T) {
	gh := newCountingGitHub(t, &countingGitHub{runsStatus: http.StatusInternalServerError})

	_, poller := newTestPoller(t, gh.URL)
	poller.PollOnce(context.Background(), &mockStore{})

	if got := counterVecValue(t, poller.metrics.ScrapeErrorsTotal, "workflows"); got != 1 {
		t.Errorf("scrape_errors_total{workflows} = %v, want 1", got)
	}
	_, jobs := gh.counts()
	if len(jobs) != 0 {
		t.Errorf("jobs were fetched despite the runs page failing: %v", jobs)
	}
}

// Options that would disable the bound they configure are ignored rather than
// obeyed: a zero budget makes the poll interval meaningless, and a zero horizon
// would collect nothing.
func TestPollerOptions_RejectNonsense(t *testing.T) {
	p := &Poller{jobBudget: DefaultJobBudgetPerPoll, horizon: CollectionHorizon}

	WithJobBudget(0)(p)
	WithJobBudget(-1)(p)
	WithHorizon(0)(p)
	WithHorizon(-time.Hour)(p)

	if p.jobBudget != DefaultJobBudgetPerPoll {
		t.Errorf("jobBudget = %d, want the default left alone", p.jobBudget)
	}
	if p.horizon != CollectionHorizon {
		t.Errorf("horizon = %v, want the default left alone", p.horizon)
	}

	WithJobBudget(7)(p)
	WithHorizon(48 * time.Hour)(p)
	if p.jobBudget != 7 || p.horizon != 48*time.Hour {
		t.Errorf("sensible values were not applied: %d, %v", p.jobBudget, p.horizon)
	}
}

func TestNewPoller_AppliesOptions(t *testing.T) {
	reg := prometheus.NewRegistry()
	p := NewPoller(nil, "org", time.Minute, NewMetrics(reg), WithJobBudget(3), WithHorizon(time.Hour))
	if p.jobBudget != 3 {
		t.Errorf("jobBudget = %d, want 3", p.jobBudget)
	}
	if p.horizon != time.Hour {
		t.Errorf("horizon = %v, want 1h", p.horizon)
	}
}

// SyncKey is what makes "unchanged" decidable. A run with no updated_at falls
// back to created_at rather than to the zero time, which would compare equal to
// the never-fetched marker and exclude the run from ever being fetched.
func TestWorkflowRun_SyncKey(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	updated := created.Add(time.Hour)

	if got := (WorkflowRun{CreatedAt: created}).SyncKey(); !got.Equal(created) {
		t.Errorf("SyncKey without updated_at = %v, want created_at %v", got, created)
	}
	if got := (WorkflowRun{CreatedAt: created, UpdatedAt: &updated}).SyncKey(); !got.Equal(updated) {
		t.Errorf("SyncKey = %v, want updated_at %v", got, updated)
	}
}
