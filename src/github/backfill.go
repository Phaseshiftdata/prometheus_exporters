package github

import (
	"context"
	"log/slog"
	"time"

	"github.com/phaseshiftdata/prometheus_exporters/src/github/collectors"
)

// The backfiller is the slow half of collection, and it is slow on purpose.
//
// On 2026-08-11 the exporter went live, collected 25 repositories, and stopped.
// Nine minutes in, github_exporter_poll_duration_seconds_count was still 0: the
// first poll had never finished. `up` was 1, /metrics answered 200, nothing was
// logged. GitHub was answering 403 -- not because the quota was gone (measured
// at 5247 of 5250 remaining while it was stuck) but because of the RATE. The
// collector issued one request per workflow run for that run's jobs, walked
// every page of every repository's history, and did it again from scratch on
// every cycle. That is the shape of request stream the secondary limiter exists
// to stop.
//
// The fix is not a bigger timeout or a longer poll interval; those make the
// failure rarer and no less certain. The fix is that historical collection
// stops being a burst inside a poll cycle and becomes a background activity
// with a bounded request rate. It runs continuously, it may take hours, and it
// says what it is doing while it does it.
//
// Why a fixed interval and not a token bucket. A bucket accumulates permission
// while nothing is happening and then allows it to be spent at once; "quiet for
// a minute, then thirty requests" is a burst, and burst is the thing that
// produced the 403. A strict minimum spacing between requests, one request per
// tick and never more, cannot save up. The rate is then a property of the loop
// rather than of a counter that might be wrong. It is also the only pacing
// scheme that can be described in one sentence to whoever is holding the pager,
// which counts for something at 2am.
//
// Why not a per-poll budget for this as well. The poll has one (see jobBudget)
// and it is the right tool there, because a poll is a bounded piece of work
// that must finish before the next one starts. Backfill is unbounded work that
// must never finish urgently. A budget-per-cycle would still deliver its
// requests as fast as the network allows -- a small burst is still a burst --
// whereas ticks are spread by construction.

// BackfillStore is what the backfiller needs from PostgreSQL. It is separate
// from StoreWriter because backfill is a distinct activity with distinct needs:
// where the poller writes what it just saw, the backfiller asks what is still
// missing and remembers where it had got to.
type BackfillStore interface {
	UpsertWorkflowRuns(ctx context.Context, runs []WorkflowRun) error
	UpsertWorkflowJobs(ctx context.Context, jobs []WorkflowJob) error
	MarkJobsSynced(ctx context.Context, runID int64, syncKey time.Time) error
	RunsMissingJobs(ctx context.Context, repo string, limit int) ([]RunJobSync, error)
	ListRepositoryNames(ctx context.Context) ([]string, error)
	LoadBackfillProgress(ctx context.Context, repo string) (BackfillProgress, error)
	SaveBackfillProgress(ctx context.Context, p BackfillProgress) error
	BackfillStats(ctx context.Context) (BackfillStats, error)
}

// BackfillProgress is how far the historical walk has got for one repository.
type BackfillProgress struct {
	Repo          string
	NextPage      int
	RunsComplete  bool
	CompletedAt   time.Time
	PagesFetched  int64
	RequestsSpent int64
}

// BackfillStats is how much work is left, across all repositories.
type BackfillStats struct {
	PendingJobRuns int64
	ReposComplete  int64
}

// rateLimitReporter is the part of *Client the backfiller reads. An interface
// rather than the concrete type so a test can say "pretend there are eleven
// requests left" without a live API.
type rateLimitReporter interface {
	RateLimitRemaining() int
}

const (
	// DefaultBackfillInterval is the minimum spacing between backfill
	// requests: 30 an hour times sixty, or 1800 requests an hour. That is
	// comfortably inside the 5000/hour primary quota with room for the polls
	// on top, and nowhere near the request rate that produced the 403.
	//
	// It also sets the pace of a cold start, which is the honest trade being
	// made here. Twenty-five repositories with a couple of hundred runs each
	// is a few thousand requests, so a first fill takes a couple of hours. An
	// exporter that is completely populated in ten minutes and then wedged for
	// nine is worth strictly less than one that is visibly filling in.
	DefaultBackfillInterval = 2 * time.Second

	// DefaultBackfillMinRateLimit is the primary-quota headroom the backfiller
	// refuses to spend. Backfill is never urgent and the poll always is, so
	// when quota runs low the background work is what gives way -- and it
	// gives way BEFORE the limit rather than after, which is the difference
	// between pausing and being 403'd.
	DefaultBackfillMinRateLimit = 500

	// DefaultBackfillRefresh is how long a completed repository is left alone
	// before its history is walked again.
	//
	// Completion is not permanent, and it should not be. Page numbers drift as
	// new runs arrive; the exporter can be down for longer than a page of
	// runs; a repository can be added to the org. Re-walking a finished
	// repository once a day costs a page or two -- and, because everything
	// fetched is inside the retention window, re-inserting what it finds is
	// idempotent. That is cheaper than any scheme for detecting the gaps
	// exactly, and it cannot leave one behind.
	DefaultBackfillRefresh = 24 * time.Hour

	// maxBackfillPages caps the walk for one repository.
	//
	// The unbounded page loop is one of the two things that made 2026-08-11
	// possible, and "the horizon will stop it" is a reason to expect
	// termination, not a guarantee of it: the horizon is enforced by a query
	// parameter and a local filter, and if both were ever wrong the loop would
	// page forever. At a hundred pages of a hundred runs this only ever binds
	// on a repository with ten thousand runs inside ninety days, which would
	// itself be worth a warning.
	maxBackfillPages = 100
)

// Backfiller walks repository history at a bounded request rate.
type Backfiller struct {
	org       string
	collector *collectors.WorkflowCollector
	client    rateLimitReporter
	metrics   *Metrics

	interval     time.Duration
	minRateLimit int
	refreshAfter time.Duration
	horizon      time.Duration

	// rotation is the repository order, and cursor is where in it the next
	// tick starts looking.
	rotation []string
	cursor   int

	nowFunc func() time.Time // for testing
}

// BackfillOption adjusts a Backfiller at construction time.
type BackfillOption func(*Backfiller)

// WithBackfillInterval sets the minimum spacing between backfill requests.
func WithBackfillInterval(d time.Duration) BackfillOption {
	return func(b *Backfiller) {
		if d > 0 {
			b.interval = d
		}
	}
}

// WithBackfillMinRateLimit sets the primary-quota headroom below which the
// backfiller stops issuing requests.
func WithBackfillMinRateLimit(n int) BackfillOption {
	return func(b *Backfiller) {
		if n >= 0 {
			b.minRateLimit = n
		}
	}
}

// WithBackfillRefresh sets how long a completed repository is left before its
// history is walked again.
func WithBackfillRefresh(d time.Duration) BackfillOption {
	return func(b *Backfiller) {
		if d > 0 {
			b.refreshAfter = d
		}
	}
}

// WithBackfillHorizon overrides how far back the walk goes. Mainly for tests;
// production wants the default, which is derived from the retention window.
func WithBackfillHorizon(d time.Duration) BackfillOption {
	return func(b *Backfiller) {
		if d > 0 {
			b.horizon = d
		}
	}
}

// NewBackfiller creates a Backfiller for one organization.
func NewBackfiller(
	client *Client, org string, metrics *Metrics, opts ...BackfillOption,
) *Backfiller {
	b := &Backfiller{
		org:          org,
		collector:    &collectors.WorkflowCollector{Client: client},
		client:       client,
		metrics:      metrics,
		interval:     DefaultBackfillInterval,
		minRateLimit: DefaultBackfillMinRateLimit,
		refreshAfter: DefaultBackfillRefresh,
		horizon:      CollectionHorizon,
		nowFunc:      time.Now,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Run ticks until the context is canceled. Every tick issues at most one
// GitHub request, which is what makes the request rate a property of the loop
// rather than of anything's good behavior.
func (b *Backfiller) Run(ctx context.Context, store BackfillStore) error {
	slog.Info("starting github backfiller",
		"interval", b.interval, "horizon", b.horizon,
		"min_rate_limit", b.minRateLimit, "refresh_after", b.refreshAfter)

	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			b.Step(ctx, store)
		}
	}
}

// Step performs one tick: at most one GitHub request, then publish where things
// stand. Exported so that a test can drive the loop without waiting on a timer.
func (b *Backfiller) Step(ctx context.Context, store BackfillStore) {
	// The heartbeat is set on every path, including the ones that do nothing.
	// A backfiller that is quiet because there is no work and one that has
	// stopped ticking are the same picture without it, and that ambiguity is
	// what made the original incident take hours.
	defer b.metrics.BackfillLastStepTimestamp.Set(float64(b.nowFunc().Unix()))
	defer b.publishStats(ctx, store)

	if !b.hasHeadroom() {
		return
	}
	b.metrics.BackfillPaused.Set(0)

	// One tick spends at most one request, but it may look at several
	// repositories to find one with work: a rotation where most repositories
	// are finished would otherwise waste most of its ticks doing nothing while
	// the one repository with a backlog is served every twenty-fifth tick. The
	// looking is two indexed queries against a local PostgreSQL; the spending
	// is a request to GitHub, and that is what is rationed.
	for range b.repoCount(ctx, store) {
		repo, ok := b.nextRepo(ctx, store)
		if !ok {
			break
		}
		if b.stepJobs(ctx, store, repo) {
			return
		}
		if b.stepRunsPage(ctx, store, repo) {
			return
		}
	}

	// Nothing to do anywhere. Said out loud, because "idle" and "stuck" have
	// to be different readings.
	b.metrics.BackfillThrottledTotal.WithLabelValues("no_work").Inc()
}

// hasHeadroom reports whether there is enough primary quota left to spend any
// of it on background work.
//
// A remaining count of zero is treated as "not observed yet" and allowed
// through, which looks wrong for about a second. The client's counter starts at
// zero and is only ever set from a response header, so before the first request
// of the process's life there is nothing to distinguish "no data" from
// "exhausted". Reading zero as exhausted would mean the backfiller never issues
// a request, therefore never sees a header, therefore stays paused forever --
// a loop that locks itself on startup. Reading it as unknown costs, at worst,
// one request into a genuinely exhausted quota, and that request is handled:
// since PR #78 the client bounds its retries, caps its sleep, and logs the
// backoff instead of hanging.
func (b *Backfiller) hasHeadroom() bool {
	remaining := b.client.RateLimitRemaining()
	if remaining > 0 && remaining < b.minRateLimit {
		b.metrics.BackfillThrottledTotal.WithLabelValues("rate_limit_headroom").Inc()
		b.metrics.BackfillPaused.Set(1)
		slog.Info("backfill paused to protect the rate limit",
			"remaining", remaining, "floor", b.minRateLimit)
		return false
	}
	return true
}

// repoCount returns how many repositories the rotation holds, refreshing it if
// it has never been loaded. It bounds the search in Step.
func (b *Backfiller) repoCount(ctx context.Context, store BackfillStore) int {
	if len(b.rotation) == 0 {
		b.refreshRotation(ctx, store)
	}
	return len(b.rotation)
}

// nextRepo returns the next repository in the rotation, reloading the rotation
// when it has been walked all the way round.
//
// Round robin rather than "finish one repository then start the next" so that
// every dashboard fills in together. Draining one repository at a time would
// have the org's largest repository monopolise the request stream for an hour
// while the other twenty-four show nothing, and nothing is the reading that
// gets a monitoring system distrusted.
func (b *Backfiller) nextRepo(ctx context.Context, store BackfillStore) (string, bool) {
	if b.cursor >= len(b.rotation) {
		b.refreshRotation(ctx, store)
	}
	if len(b.rotation) == 0 {
		return "", false
	}
	repo := b.rotation[b.cursor]
	b.cursor++
	return repo, true
}

func (b *Backfiller) refreshRotation(ctx context.Context, store BackfillStore) {
	names, err := store.ListRepositoryNames(ctx)
	if err != nil {
		slog.Error("backfill could not list repositories", "error", err)
		b.metrics.ScrapeErrorsTotal.WithLabelValues("backfill").Inc()
		return
	}
	b.rotation = names
	b.cursor = 0
	b.metrics.BackfillReposTotal.Set(float64(len(names)))
}

// stepJobs fetches the jobs of one run that is missing them. It reports whether
// the tick's request was spent.
//
// Jobs come before deeper run pages because a run without its jobs is a hole in
// data the exporter already claims to have -- workflow_jobs_daily is what the
// slow-build panels read -- whereas an unfetched page is data nobody has been
// promised yet.
func (b *Backfiller) stepJobs(ctx context.Context, store BackfillStore, repo string) bool {
	pending, err := store.RunsMissingJobs(ctx, repo, 1)
	if err != nil {
		slog.Error("backfill could not read pending jobs", "repo", repo, "error", err)
		b.metrics.ScrapeErrorsTotal.WithLabelValues("backfill").Inc()
		return false
	}
	if len(pending) == 0 {
		return false
	}
	run := pending[0]

	cJobs, err := b.collector.CollectJobs(ctx, b.org, repo, run.RunID)
	b.metrics.APIRequestsTotal.WithLabelValues("backfill", "jobs").Inc()
	if err != nil {
		slog.Warn("backfill jobs request failed",
			"repo", repo, "run", run.RunID, "error", err)
		b.metrics.ScrapeErrorsTotal.WithLabelValues("backfill").Inc()
		return true
	}

	jobs := convertWorkflowJobs(cJobs)
	if err := store.UpsertWorkflowJobs(ctx, jobs); err != nil {
		slog.Error("backfill could not store jobs", "repo", repo, "run", run.RunID, "error", err)
		b.metrics.ScrapeErrorsTotal.WithLabelValues("backfill").Inc()
		return true
	}

	// Marked only after the jobs are stored. Dying in between costs one
	// repeated request; marking first would lose the jobs for good.
	if err := store.MarkJobsSynced(ctx, run.RunID, run.SyncKey); err != nil {
		slog.Error("backfill could not mark jobs synced",
			"repo", repo, "run", run.RunID, "error", err)
		b.metrics.ScrapeErrorsTotal.WithLabelValues("backfill").Inc()
		return true
	}

	slog.Debug("backfilled jobs", "repo", repo, "run", run.RunID, "jobs", len(jobs))
	return true
}

// stepRunsPage fetches one more page of a repository's run history. It reports
// whether the tick's request was spent.
func (b *Backfiller) stepRunsPage(ctx context.Context, store BackfillStore, repo string) bool {
	prog, err := store.LoadBackfillProgress(ctx, repo)
	if err != nil {
		slog.Error("backfill could not read progress", "repo", repo, "error", err)
		b.metrics.ScrapeErrorsTotal.WithLabelValues("backfill").Inc()
		return false
	}
	prog.Repo = repo

	now := b.nowFunc()
	if prog.RunsComplete {
		if now.Sub(prog.CompletedAt) < b.refreshAfter {
			return false
		}
		slog.Info("re-opening completed backfill",
			"repo", repo, "completed_at", prog.CompletedAt)
		prog.RunsComplete = false
		prog.NextPage = 1
	}
	if prog.NextPage < 1 {
		prog.NextPage = 1
	}

	page, err := b.collector.CollectRunsPage(ctx, b.org, repo, collectors.RunQuery{
		Page:         prog.NextPage,
		CreatedSince: now.Add(-b.horizon),
	})
	b.metrics.APIRequestsTotal.WithLabelValues("backfill", "runs_page").Inc()
	prog.RequestsSpent++
	if err != nil {
		slog.Warn("backfill runs request failed",
			"repo", repo, "page", prog.NextPage, "error", err)
		b.metrics.ScrapeErrorsTotal.WithLabelValues("backfill").Inc()
		// Progress is still saved: the request was spent and the count of what
		// it cost should say so, and NextPage is unchanged so the same page is
		// retried on this repository's next turn rather than skipped.
		b.saveProgress(ctx, store, prog)
		return true
	}

	if len(page.Runs) > 0 {
		if err := store.UpsertWorkflowRuns(ctx, convertWorkflowRuns(page.Runs)); err != nil {
			slog.Error("backfill could not store runs", "repo", repo, "error", err)
			b.metrics.ScrapeErrorsTotal.WithLabelValues("backfill").Inc()
			b.saveProgress(ctx, store, prog)
			return true
		}
		prog.PagesFetched++
	}

	switch {
	case page.More && prog.NextPage < maxBackfillPages:
		// A 304 lands here too, with More false -- see below. A full page
		// means there may be another.
		prog.NextPage++
	case page.NotModified:
		// The page has not changed since it was last fetched, so its runs are
		// already stored; move on rather than concluding anything about the
		// end of history, which an unchanged page says nothing about.
		prog.NextPage++
	default:
		if prog.NextPage >= maxBackfillPages {
			slog.Warn("backfill hit the page cap; treating history as complete",
				"repo", repo, "pages", prog.NextPage)
		}
		prog.RunsComplete = true
		prog.CompletedAt = now
		prog.NextPage = 1
		slog.Info("backfill reached the horizon for repository",
			"repo", repo, "pages_fetched", prog.PagesFetched,
			"requests_spent", prog.RequestsSpent)
	}

	b.saveProgress(ctx, store, prog)
	return true
}

func (b *Backfiller) saveProgress(ctx context.Context, store BackfillStore, p BackfillProgress) {
	if err := store.SaveBackfillProgress(ctx, p); err != nil {
		slog.Error("backfill could not save progress", "repo", p.Repo, "error", err)
		b.metrics.ScrapeErrorsTotal.WithLabelValues("backfill").Inc()
	}
}

// publishStats republishes the backlog gauges. It runs on every tick, including
// the ones that spent no request, because the number that matters most is how
// much is left -- and a gauge that only moves when work happens cannot
// distinguish "finished" from "stopped".
func (b *Backfiller) publishStats(ctx context.Context, store BackfillStore) {
	stats, err := store.BackfillStats(ctx)
	if err != nil {
		slog.Error("backfill could not read stats", "error", err)
		b.metrics.ScrapeErrorsTotal.WithLabelValues("backfill").Inc()
		return
	}
	b.metrics.BackfillPendingJobRuns.Set(float64(stats.PendingJobRuns))
	b.metrics.BackfillReposComplete.Set(float64(stats.ReposComplete))
}
