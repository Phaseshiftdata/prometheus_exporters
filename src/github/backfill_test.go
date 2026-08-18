package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/phaseshiftdata/prometheus_exporters/src/github/collectors"
)

// The backfiller's contract is arithmetic: one tick, at most one request, no
// saving up. Every test here is a way of asking whether that still holds, and
// it holds or 2026-08-11 happens again -- a first poll that never finished
// because history was walked as a burst, one request per workflow run, from
// page one, every cycle.

// ---- fakes

type fakeAPI struct {
	mu   sync.Mutex
	urls []string

	// runsPages maps a page number to the run ids it returns.
	runsPages map[int][]int64
	// fullPages are page numbers that should look full (100 runs) so that the
	// walk believes another page may exist.
	fullPages   map[int]bool
	runsErr     error
	jobsErr     error
	notModified bool
}

func (f *fakeAPI) Get(_ context.Context, rawURL string, result any) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.urls = append(f.urls, rawURL)

	if strings.HasSuffix(rawURL, "/jobs") {
		if f.jobsErr != nil {
			return false, f.jobsErr
		}
		runID := runIDFromJobsURL(rawURL)
		return decodeInto(result, map[string]any{"jobs": []map[string]any{{
			"id": runID * 10, "run_id": runID, "name": "build", "conclusion": "success",
			"started_at": "2026-01-01T00:00:00Z", "completed_at": "2026-01-01T00:05:00Z",
		}}})
	}

	if f.runsErr != nil {
		return false, f.runsErr
	}
	if f.notModified {
		return false, nil
	}

	page := pageFromURL(rawURL)
	ids := f.runsPages[page]
	runs := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		runs = append(runs, map[string]any{
			"id": id, "name": "CI", "head_branch": "main", "conclusion": "success",
			"run_attempt": 1, "created_at": "2026-08-10T00:00:00Z",
			"run_started_at": "2026-08-10T00:00:01Z", "updated_at": "2026-08-10T00:01:00Z",
		})
	}
	// A full page is what tells the walk another page may exist. Padding to
	// the real page size keeps the test honest about how that is judged.
	if f.fullPages[page] {
		for len(runs) < collectors.RunsPerPage {
			runs = append(runs, map[string]any{
				"id": int64(900000 + len(runs)), "name": "CI", "head_branch": "main",
				"conclusion": "success", "run_attempt": 1,
				"created_at": "2026-08-10T00:00:00Z",
			})
		}
	}
	return decodeInto(result, map[string]any{"workflow_runs": runs})
}

func decodeInto(result any, payload any) (bool, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return false, err
	}
	return true, nil
}

func pageFromURL(rawURL string) int {
	u, err := url.Parse(rawURL)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(u.Query().Get("page"))
	return n
}

func runIDFromJobsURL(rawURL string) int64 {
	parts := strings.Split(strings.Trim(rawURL, "/"), "/")
	if len(parts) < 2 {
		return 0
	}
	n, _ := strconv.ParseInt(parts[len(parts)-2], 10, 64)
	return n
}

func (f *fakeAPI) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.urls...)
}

type fakeRateLimit struct{ remaining int }

func (f fakeRateLimit) RateLimitRemaining() int { return f.remaining }

type fakeBackfillStore struct {
	mu sync.Mutex

	repos    []string
	reposErr error

	pending    map[string][]RunJobSync
	pendingErr error

	progress map[string]BackfillProgress
	loadErr  error
	saveErr  error
	saved    []BackfillProgress

	stats    BackfillStats
	statsErr error

	runs    []WorkflowRun
	runsErr error
	jobs    []WorkflowJob
	jobsErr error
	synced  []RunJobSync
	markErr error
}

func newFakeBackfillStore(repos ...string) *fakeBackfillStore {
	return &fakeBackfillStore{
		repos:    repos,
		pending:  map[string][]RunJobSync{},
		progress: map[string]BackfillProgress{},
	}
}

func (s *fakeBackfillStore) UpsertWorkflowRuns(_ context.Context, runs []WorkflowRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs = append(s.runs, runs...)
	return s.runsErr
}

func (s *fakeBackfillStore) UpsertWorkflowJobs(_ context.Context, jobs []WorkflowJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, jobs...)
	return s.jobsErr
}

func (s *fakeBackfillStore) MarkJobsSynced(_ context.Context, runID int64, key time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.synced = append(s.synced, RunJobSync{RunID: runID, SyncKey: key})
	if s.markErr == nil {
		// A marked run leaves the queue, exactly as the SQL predicate would.
		for repo, runs := range s.pending {
			kept := runs[:0]
			for _, r := range runs {
				if r.RunID != runID {
					kept = append(kept, r)
				}
			}
			s.pending[repo] = kept
		}
	}
	return s.markErr
}

func (s *fakeBackfillStore) RunsMissingJobs(_ context.Context, repo string, limit int) ([]RunJobSync, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingErr != nil {
		return nil, s.pendingErr
	}
	runs := s.pending[repo]
	if len(runs) > limit {
		runs = runs[:limit]
	}
	return append([]RunJobSync(nil), runs...), nil
}

func (s *fakeBackfillStore) ListRepositoryNames(_ context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reposErr != nil {
		return nil, s.reposErr
	}
	return append([]string(nil), s.repos...), nil
}

func (s *fakeBackfillStore) LoadBackfillProgress(_ context.Context, repo string) (BackfillProgress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return BackfillProgress{}, s.loadErr
	}
	p, ok := s.progress[repo]
	if !ok {
		return BackfillProgress{Repo: repo, NextPage: 1}, nil
	}
	return p, nil
}

func (s *fakeBackfillStore) SaveBackfillProgress(_ context.Context, p BackfillProgress) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved = append(s.saved, p)
	if s.saveErr != nil {
		return s.saveErr
	}
	s.progress[p.Repo] = p
	return nil
}

func (s *fakeBackfillStore) BackfillStats(_ context.Context) (BackfillStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.statsErr != nil {
		return BackfillStats{}, s.statsErr
	}
	return s.stats, nil
}

func (s *fakeBackfillStore) lastProgress(repo string) BackfillProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress[repo]
}

// newTestBackfiller wires a Backfiller onto fakes, with a fixed clock.
func newTestBackfiller(t *testing.T, api *fakeAPI, remaining int) (*Backfiller, *Metrics) {
	t.Helper()
	metrics := NewMetrics(prometheus.NewRegistry())
	b := &Backfiller{
		org:          "test-org",
		collector:    &collectors.WorkflowCollector{Client: api},
		client:       fakeRateLimit{remaining: remaining},
		metrics:      metrics,
		interval:     time.Millisecond,
		minRateLimit: DefaultBackfillMinRateLimit,
		refreshAfter: DefaultBackfillRefresh,
		horizon:      CollectionHorizon,
		nowFunc:      func() time.Time { return time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC) },
	}
	return b, metrics
}

// ---- the pacing contract

// One tick, one request. Not "one request per repository", not "as many as the
// backlog needs" -- one. This is the entire difference from the collector that
// tripped the secondary limiter.
func TestBackfiller_StepSpendsOneRequestEvenWithAHugeBacklog(t *testing.T) {
	api := &fakeAPI{runsPages: map[int][]int64{1: {1, 2, 3}}}
	b, _ := newTestBackfiller(t, api, 5000)

	store := newFakeBackfillStore("repo-a", "repo-b", "repo-c")
	for _, repo := range store.repos {
		for i := range 50 {
			store.pending[repo] = append(store.pending[repo],
				RunJobSync{RunID: int64(1000 + i), SyncKey: time.Unix(int64(i), 0).UTC()})
		}
	}

	b.Step(context.Background(), store)

	if got := len(api.requests()); got != 1 {
		t.Fatalf("one tick issued %d requests, want 1: %v", got, api.requests())
	}
}

// Ten ticks, ten requests, spread across the rotation rather than draining one
// repository while the others show nothing.
func TestBackfiller_RotatesAcrossRepositories(t *testing.T) {
	api := &fakeAPI{runsPages: map[int][]int64{}}
	b, _ := newTestBackfiller(t, api, 5000)

	store := newFakeBackfillStore("repo-a", "repo-b")
	for _, repo := range store.repos {
		for i := range 5 {
			store.pending[repo] = append(store.pending[repo],
				RunJobSync{RunID: int64(len(repo)*1000 + i), SyncKey: time.Unix(0, 0).UTC()})
		}
	}

	for range 4 {
		b.Step(context.Background(), store)
	}

	var a, bb int
	for _, u := range api.requests() {
		switch {
		case strings.Contains(u, "/repo-a/"):
			a++
		case strings.Contains(u, "/repo-b/"):
			bb++
		}
	}
	if a != 2 || bb != 2 {
		t.Errorf("requests were not shared out: repo-a=%d repo-b=%d (%v)", a, bb, api.requests())
	}
}

// Jobs first: a run without its jobs is a hole in data the exporter already
// claims to have, whereas an unfetched page is data nobody has been promised.
func TestBackfiller_PrefersMissingJobsOverDeeperPages(t *testing.T) {
	api := &fakeAPI{runsPages: map[int][]int64{1: {7}}}
	b, _ := newTestBackfiller(t, api, 5000)

	store := newFakeBackfillStore("repo-a")
	store.pending["repo-a"] = []RunJobSync{{RunID: 42, SyncKey: time.Unix(99, 0).UTC()}}

	b.Step(context.Background(), store)

	reqs := api.requests()
	if len(reqs) != 1 || !strings.HasSuffix(reqs[0], "/actions/runs/42/jobs") {
		t.Fatalf("expected a jobs request for run 42, got %v", reqs)
	}
	if len(store.jobs) != 1 || store.jobs[0].RunID != 42 {
		t.Fatalf("jobs were not stored: %+v", store.jobs)
	}
	// Marked only after the jobs are stored, and with the run's own sync key.
	if len(store.synced) != 1 || store.synced[0].RunID != 42 ||
		!store.synced[0].SyncKey.Equal(time.Unix(99, 0).UTC()) {
		t.Fatalf("run was not marked synced correctly: %+v", store.synced)
	}
}

// A tick may look at several repositories to find work, but it still spends at
// most one request. Without this, a rotation where most repositories are
// finished would serve the one with a backlog every twenty-fifth tick.
func TestBackfiller_SkipsIdleRepositoriesWithinOneTick(t *testing.T) {
	api := &fakeAPI{runsPages: map[int][]int64{}}
	b, _ := newTestBackfiller(t, api, 5000)

	store := newFakeBackfillStore("done-a", "done-b", "busy")
	for _, repo := range []string{"done-a", "done-b"} {
		store.progress[repo] = BackfillProgress{
			Repo: repo, NextPage: 1, RunsComplete: true,
			CompletedAt: b.nowFunc(),
		}
	}
	store.pending["busy"] = []RunJobSync{{RunID: 5, SyncKey: time.Unix(1, 0).UTC()}}

	b.Step(context.Background(), store)

	reqs := api.requests()
	if len(reqs) != 1 || !strings.Contains(reqs[0], "/busy/") {
		t.Fatalf("expected exactly one request, to the busy repository: %v", reqs)
	}
}

// ---- progress across a restart

// The walk resumes where it stopped. A restart that begins at page 1 again is
// how a bounded amount of history turns into an unbounded amount of requests.
func TestBackfiller_ResumesFromStoredProgress(t *testing.T) {
	api := &fakeAPI{runsPages: map[int][]int64{4: {41, 42}}}
	b, _ := newTestBackfiller(t, api, 5000)

	store := newFakeBackfillStore("repo-a")
	store.progress["repo-a"] = BackfillProgress{Repo: "repo-a", NextPage: 4, PagesFetched: 3}

	b.Step(context.Background(), store)

	reqs := api.requests()
	if len(reqs) != 1 || pageFromURL(reqs[0]) != 4 {
		t.Fatalf("expected a request for page 4, got %v", reqs)
	}
	if len(store.runs) != 2 {
		t.Fatalf("expected the page's runs to be stored, got %d", len(store.runs))
	}
	// A short page means the horizon has been reached.
	p := store.lastProgress("repo-a")
	if !p.RunsComplete {
		t.Error("a short page should complete the walk")
	}
	if !p.CompletedAt.Equal(b.nowFunc()) {
		t.Errorf("CompletedAt = %v, want the current time", p.CompletedAt)
	}
	if p.NextPage != 1 {
		t.Errorf("NextPage = %d, want 1 so the next sweep starts from the top", p.NextPage)
	}
	if p.PagesFetched != 4 || p.RequestsSpent != 1 {
		t.Errorf("progress accounting is wrong: %+v", p)
	}
}

// Progress is written after EVERY page, not at the end of the repository. The
// end may be hours away, and a restart in between must not lose the pages
// already paid for.
func TestBackfiller_SavesProgressAfterEachPage(t *testing.T) {
	api := &fakeAPI{
		runsPages: map[int][]int64{1: {1}, 2: {2}},
		fullPages: map[int]bool{1: true},
	}
	b, _ := newTestBackfiller(t, api, 5000)
	store := newFakeBackfillStore("repo-a")

	b.Step(context.Background(), store)
	if p := store.lastProgress("repo-a"); p.NextPage != 2 || p.RunsComplete {
		t.Fatalf("after a full page, want page 2 and incomplete, got %+v", p)
	}

	b.Step(context.Background(), store)
	if p := store.lastProgress("repo-a"); !p.RunsComplete {
		t.Fatalf("after a short page, want complete, got %+v", p)
	}
	if len(store.saved) != 2 {
		t.Errorf("progress was saved %d times, want once per page", len(store.saved))
	}
}

// Completion is not permanent. Page numbers drift, the exporter can be down
// longer than a page of runs, and repositories are added; a completed walk is
// re-opened after the refresh interval, which is safe because everything
// fetched is inside the retention window and re-inserting it is idempotent.
func TestBackfiller_ReopensACompletedWalkAfterTheRefreshInterval(t *testing.T) {
	api := &fakeAPI{runsPages: map[int][]int64{1: {1}}}
	b, _ := newTestBackfiller(t, api, 5000)
	now := b.nowFunc()

	store := newFakeBackfillStore("repo-a")
	store.progress["repo-a"] = BackfillProgress{
		Repo: "repo-a", NextPage: 1, RunsComplete: true,
		CompletedAt: now.Add(-b.refreshAfter + time.Minute),
	}

	b.Step(context.Background(), store)
	if got := len(api.requests()); got != 0 {
		t.Fatalf("a recently completed repository should be left alone, got %d requests", got)
	}

	store.progress["repo-a"] = BackfillProgress{
		Repo: "repo-a", NextPage: 1, RunsComplete: true,
		CompletedAt: now.Add(-b.refreshAfter - time.Minute),
	}
	b.Step(context.Background(), store)
	if got := len(api.requests()); got != 1 {
		t.Fatalf("a stale completion should be re-opened, got %d requests", got)
	}
}

// An unchanged page says nothing about where history ends, so the walk moves on
// rather than concluding it has finished.
func TestBackfiller_NotModifiedPageAdvancesRatherThanCompletes(t *testing.T) {
	api := &fakeAPI{notModified: true}
	b, _ := newTestBackfiller(t, api, 5000)
	store := newFakeBackfillStore("repo-a")

	b.Step(context.Background(), store)

	p := store.lastProgress("repo-a")
	if p.RunsComplete {
		t.Error("a 304 must not be read as the end of history")
	}
	if p.NextPage != 2 {
		t.Errorf("NextPage = %d, want 2", p.NextPage)
	}
}

// The page loop is capped. "The horizon will stop it" is a reason to expect
// termination, not a guarantee -- and an uncapped page loop is one of the two
// things that made 2026-08-11 possible.
func TestBackfiller_StopsAtThePageCap(t *testing.T) {
	api := &fakeAPI{
		runsPages: map[int][]int64{maxBackfillPages: {1}},
		fullPages: map[int]bool{maxBackfillPages: true},
	}
	b, _ := newTestBackfiller(t, api, 5000)

	store := newFakeBackfillStore("repo-a")
	store.progress["repo-a"] = BackfillProgress{Repo: "repo-a", NextPage: maxBackfillPages}

	b.Step(context.Background(), store)

	if p := store.lastProgress("repo-a"); !p.RunsComplete {
		t.Errorf("the walk should stop at the cap, got %+v", p)
	}
}

// A page number that somehow arrives below 1 is asked for as page 1 rather than
// refused, because refusing would stall that repository forever.
func TestBackfiller_NormalisesAnImpossiblePageNumber(t *testing.T) {
	api := &fakeAPI{runsPages: map[int][]int64{1: {1}}}
	b, _ := newTestBackfiller(t, api, 5000)

	store := newFakeBackfillStore("repo-a")
	store.progress["repo-a"] = BackfillProgress{Repo: "repo-a", NextPage: 0}

	b.Step(context.Background(), store)

	reqs := api.requests()
	if len(reqs) != 1 || pageFromURL(reqs[0]) != 1 {
		t.Fatalf("expected page 1, got %v", reqs)
	}
}

// The horizon travels with every backfill request too, not just the poll's.
func TestBackfiller_AsksWithinTheHorizon(t *testing.T) {
	api := &fakeAPI{runsPages: map[int][]int64{1: {1}}}
	b, _ := newTestBackfiller(t, api, 5000)
	store := newFakeBackfillStore("repo-a")

	b.Step(context.Background(), store)

	u, err := url.Parse(api.requests()[0])
	if err != nil {
		t.Fatalf("unparseable URL: %v", err)
	}
	want := ">=" + b.nowFunc().Add(-CollectionHorizon).Format("2006-01-02")
	if got := u.Query().Get("created"); got != want {
		t.Errorf("created = %q, want %q", got, want)
	}
}

// ---- rate limit protection

// Backing off BEFORE the limit rather than after. Backfill is never urgent and
// the poll always is, so the background work is what gives way.
func TestBackfiller_PausesBeforeTheRateLimitIsReached(t *testing.T) {
	api := &fakeAPI{runsPages: map[int][]int64{1: {1}}}
	b, metrics := newTestBackfiller(t, api, 100) // floor is 500
	store := newFakeBackfillStore("repo-a")

	b.Step(context.Background(), store)

	if got := len(api.requests()); got != 0 {
		t.Fatalf("backfill spent %d requests below the floor, want 0", got)
	}
	if got := counterVecValue(t, metrics.BackfillThrottledTotal, "rate_limit_headroom"); got != 1 {
		t.Errorf("backfill_throttled_total{rate_limit_headroom} = %v, want 1", got)
	}
	if got := gaugeValue(t, metrics.BackfillPaused); got != 1 {
		t.Errorf("backfill_paused = %v, want 1", got)
	}
}

// Zero remaining means "no response seen yet" as well as "exhausted", and the
// client cannot tell them apart. Treating it as exhausted would mean the
// backfiller never issues a request, never sees a header, and stays paused
// forever -- a loop that locks itself on startup.
func TestBackfiller_ZeroRemainingIsUnknownNotExhausted(t *testing.T) {
	api := &fakeAPI{runsPages: map[int][]int64{1: {1}}}
	b, metrics := newTestBackfiller(t, api, 0)
	store := newFakeBackfillStore("repo-a")

	b.Step(context.Background(), store)

	if got := len(api.requests()); got != 1 {
		t.Fatalf("an unobserved rate limit should not pause the backfiller: %d requests", got)
	}
	if got := gaugeValue(t, metrics.BackfillPaused); got != 0 {
		t.Errorf("backfill_paused = %v, want 0", got)
	}
}

// ---- observability

func TestBackfiller_PublishesProgressAndHeartbeatOnEveryTick(t *testing.T) {
	api := &fakeAPI{runsPages: map[int][]int64{1: {1}}}
	b, metrics := newTestBackfiller(t, api, 100) // paused: even then, it reports
	store := newFakeBackfillStore("repo-a")
	store.stats = BackfillStats{PendingJobRuns: 17, ReposComplete: 3}

	b.Step(context.Background(), store)

	if got := gaugeValue(t, metrics.BackfillPendingJobRuns); got != 17 {
		t.Errorf("backfill_pending_job_runs = %v, want 17", got)
	}
	if got := gaugeValue(t, metrics.BackfillReposComplete); got != 3 {
		t.Errorf("backfill_repos_complete = %v, want 3", got)
	}
	if got := gaugeValue(t, metrics.BackfillLastStepTimestamp); got != float64(b.nowFunc().Unix()) {
		t.Errorf("backfill_last_step_timestamp_seconds = %v, want the tick's time", got)
	}
}

func TestBackfiller_CountsItsRequests(t *testing.T) {
	api := &fakeAPI{runsPages: map[int][]int64{1: {1}}}
	b, metrics := newTestBackfiller(t, api, 5000)

	store := newFakeBackfillStore("repo-a")
	store.pending["repo-a"] = []RunJobSync{{RunID: 8, SyncKey: time.Unix(1, 0).UTC()}}

	b.Step(context.Background(), store) // jobs
	b.Step(context.Background(), store) // runs page, the queue having drained

	if got := counterVecValue(t, metrics.APIRequestsTotal, "backfill", "jobs"); got != 1 {
		t.Errorf("api_requests_total{backfill,jobs} = %v, want 1", got)
	}
	if got := counterVecValue(t, metrics.APIRequestsTotal, "backfill", "runs_page"); got != 1 {
		t.Errorf("api_requests_total{backfill,runs_page} = %v, want 1", got)
	}
	if got := gaugeValue(t, metrics.BackfillReposTotal); got != 1 {
		t.Errorf("backfill_repos_total = %v, want 1", got)
	}
}

// Idle and stuck have to be different readings.
func TestBackfiller_SaysSoWhenThereIsNothingToDo(t *testing.T) {
	api := &fakeAPI{runsPages: map[int][]int64{}}
	b, metrics := newTestBackfiller(t, api, 5000)

	store := newFakeBackfillStore("repo-a")
	store.progress["repo-a"] = BackfillProgress{
		Repo: "repo-a", NextPage: 1, RunsComplete: true, CompletedAt: b.nowFunc(),
	}

	b.Step(context.Background(), store)

	if got := counterVecValue(t, metrics.BackfillThrottledTotal, "no_work"); got != 1 {
		t.Errorf("backfill_throttled_total{no_work} = %v, want 1", got)
	}
	if got := len(api.requests()); got != 0 {
		t.Errorf("expected no requests, got %d", got)
	}
}

// The rotation can empty out underneath a tick -- a repository leaves the org,
// or the listing fails on the refresh. The tick must end, not index into a
// slice that is no longer there.
func TestBackfiller_RotationEmptyingMidTickIsSurvived(t *testing.T) {
	api := &fakeAPI{}
	b, metrics := newTestBackfiller(t, api, 5000)

	// A rotation already walked to the end, and a store with nothing in it:
	// the refresh inside nextRepo returns an empty list.
	b.rotation = []string{"repo-a"}
	b.cursor = 1

	b.Step(context.Background(), newFakeBackfillStore())

	if got := len(api.requests()); got != 0 {
		t.Fatalf("expected no requests, got %d", got)
	}
	if got := counterVecValue(t, metrics.BackfillThrottledTotal, "no_work"); got != 1 {
		t.Errorf("backfill_throttled_total{no_work} = %v, want 1", got)
	}
}

func TestBackfiller_NoRepositoriesYet(t *testing.T) {
	api := &fakeAPI{}
	b, metrics := newTestBackfiller(t, api, 5000)

	b.Step(context.Background(), newFakeBackfillStore())

	if got := counterVecValue(t, metrics.BackfillThrottledTotal, "no_work"); got != 1 {
		t.Errorf("backfill_throttled_total{no_work} = %v, want 1", got)
	}
}

// ---- failure paths: everything is counted, nothing panics, nothing retries
// harder than it should.

func TestBackfiller_FailuresAreCountedAndSurvived(t *testing.T) {
	cases := []struct {
		name    string
		api     *fakeAPI
		arrange func(*fakeBackfillStore)
	}{
		{
			name: "cannot list repositories",
			api:  &fakeAPI{},
			arrange: func(s *fakeBackfillStore) {
				s.reposErr = errors.New("connection reset")
			},
		},
		{
			name: "cannot read the pending queue",
			api:  &fakeAPI{runsPages: map[int][]int64{1: {1}}},
			arrange: func(s *fakeBackfillStore) {
				s.pendingErr = errors.New("query failed")
			},
		},
		{
			name: "cannot read progress",
			api:  &fakeAPI{},
			arrange: func(s *fakeBackfillStore) {
				s.loadErr = errors.New("query failed")
			},
		},
		{
			name: "jobs request fails",
			api:  &fakeAPI{jobsErr: errors.New("403 forbidden")},
			arrange: func(s *fakeBackfillStore) {
				s.pending["repo-a"] = []RunJobSync{{RunID: 1, SyncKey: time.Unix(1, 0).UTC()}}
			},
		},
		{
			name:    "runs request fails",
			api:     &fakeAPI{runsErr: errors.New("403 forbidden")},
			arrange: func(_ *fakeBackfillStore) {},
		},
		{
			name: "cannot store jobs",
			api:  &fakeAPI{},
			arrange: func(s *fakeBackfillStore) {
				s.pending["repo-a"] = []RunJobSync{{RunID: 1, SyncKey: time.Unix(1, 0).UTC()}}
				s.jobsErr = errors.New("deadlock")
			},
		},
		{
			name: "cannot mark synced",
			api:  &fakeAPI{},
			arrange: func(s *fakeBackfillStore) {
				s.pending["repo-a"] = []RunJobSync{{RunID: 1, SyncKey: time.Unix(1, 0).UTC()}}
				s.markErr = errors.New("deadlock")
			},
		},
		{
			name: "cannot store runs",
			api:  &fakeAPI{runsPages: map[int][]int64{1: {1}}},
			arrange: func(s *fakeBackfillStore) {
				s.runsErr = errors.New("deadlock")
			},
		},
		{
			name: "cannot save progress",
			api:  &fakeAPI{runsPages: map[int][]int64{1: {1}}},
			arrange: func(s *fakeBackfillStore) {
				s.saveErr = errors.New("deadlock")
			},
		},
		{
			name: "cannot read stats",
			api:  &fakeAPI{runsPages: map[int][]int64{1: {1}}},
			arrange: func(s *fakeBackfillStore) {
				s.statsErr = errors.New("query failed")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, metrics := newTestBackfiller(t, tc.api, 5000)
			store := newFakeBackfillStore("repo-a")
			tc.arrange(store)

			b.Step(context.Background(), store)

			if got := counterVecValue(t, metrics.ScrapeErrorsTotal, "backfill"); got < 1 {
				t.Errorf("scrape_errors_total{backfill} = %v, want at least 1", got)
			}
			if got := gaugeValue(t, metrics.BackfillLastStepTimestamp); got == 0 {
				t.Error("the heartbeat must move even on a failed tick")
			}
		})
	}
}

// A failed runs request retries the SAME page next time rather than skipping
// it, because a skipped page is a hole nothing else will fill.
func TestBackfiller_FailedPageIsRetriedNotSkipped(t *testing.T) {
	api := &fakeAPI{runsErr: errors.New("500")}
	b, _ := newTestBackfiller(t, api, 5000)

	store := newFakeBackfillStore("repo-a")
	store.progress["repo-a"] = BackfillProgress{Repo: "repo-a", NextPage: 3}

	b.Step(context.Background(), store)

	p := store.lastProgress("repo-a")
	if p.NextPage != 3 {
		t.Errorf("NextPage = %d, want 3 (the failed page, retried)", p.NextPage)
	}
	if p.RequestsSpent != 1 {
		t.Errorf("RequestsSpent = %d, want 1 -- a failed request still cost one", p.RequestsSpent)
	}
}

// ---- construction and the loop itself

func TestNewBackfiller_DefaultsAndOptions(t *testing.T) {
	metrics := NewMetrics(prometheus.NewRegistry())
	client := NewClient(NewTestAuth("t", time.Now().Add(time.Hour)))

	b := NewBackfiller(client, "org", metrics)
	if b.interval != DefaultBackfillInterval || b.minRateLimit != DefaultBackfillMinRateLimit ||
		b.refreshAfter != DefaultBackfillRefresh || b.horizon != CollectionHorizon {
		t.Fatalf("unexpected defaults: %+v", b)
	}

	b = NewBackfiller(client, "org", metrics,
		WithBackfillInterval(5*time.Second),
		WithBackfillMinRateLimit(10),
		WithBackfillRefresh(2*time.Hour),
		WithBackfillHorizon(48*time.Hour),
	)
	if b.interval != 5*time.Second || b.minRateLimit != 10 ||
		b.refreshAfter != 2*time.Hour || b.horizon != 48*time.Hour {
		t.Fatalf("options were not applied: %+v", b)
	}

	// Values that would remove the bound are ignored rather than obeyed. A
	// zero interval is an unpaced loop, which is the original failure.
	b = NewBackfiller(client, "org", metrics,
		WithBackfillInterval(0),
		WithBackfillMinRateLimit(-1),
		WithBackfillRefresh(0),
		WithBackfillHorizon(0),
	)
	if b.interval != DefaultBackfillInterval || b.minRateLimit != DefaultBackfillMinRateLimit ||
		b.refreshAfter != DefaultBackfillRefresh || b.horizon != CollectionHorizon {
		t.Fatalf("nonsense options were applied: %+v", b)
	}

	// Zero headroom is allowed: it means "never pause", which is a legitimate
	// choice for an installation whose App does nothing else.
	b = NewBackfiller(client, "org", metrics, WithBackfillMinRateLimit(0))
	if b.minRateLimit != 0 {
		t.Errorf("minRateLimit = %d, want 0", b.minRateLimit)
	}
}

func TestBackfiller_RunTicksUntilCancelled(t *testing.T) {
	api := &fakeAPI{runsPages: map[int][]int64{}}
	b, _ := newTestBackfiller(t, api, 5000)
	b.interval = 5 * time.Millisecond

	store := newFakeBackfillStore("repo-a")
	store.pending["repo-a"] = []RunJobSync{
		{RunID: 1, SyncKey: time.Unix(1, 0).UTC()},
		{RunID: 2, SyncKey: time.Unix(2, 0).UTC()},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	err := b.Run(ctx, store)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run returned %v, want DeadlineExceeded", err)
	}
	if got := len(api.requests()); got == 0 {
		t.Fatal("Run issued no requests at all")
	}
	// The pace is a property of the loop: in 40ms of 5ms ticks nothing can
	// have issued more than a handful of requests, however deep the backlog.
	if got := len(api.requests()); got > 10 {
		t.Errorf("Run issued %d requests in 40ms; the interval is not being honored", got)
	}
}
