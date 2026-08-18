package github

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/phaseshiftdata/prometheus_exporters/src/github/collectors"
)

// StoreWriter is the interface that the poller uses to persist collected data.
// It is intentionally narrow to allow easy mocking in tests.
//
// The last two methods are reads, which is a slightly odd thing for something
// called a writer to have, and they are worth the oddity. Before them the
// poller had no way to know what it had already collected, so it asked GitHub
// for all of it, every cycle: one request per workflow run for that run's jobs,
// across every run in every repository's history. On 2026-08-11 that burst
// tripped GitHub's secondary rate limit and the first poll never finished.
// SelectRunsForJobFetch is how the poller stops asking questions PostgreSQL can
// already answer.
type StoreWriter interface {
	UpsertRepositories(ctx context.Context, repos []Repository) error
	UpsertWorkflowRuns(ctx context.Context, runs []WorkflowRun) error
	UpsertWorkflowJobs(ctx context.Context, jobs []WorkflowJob) error
	UpsertPullRequests(ctx context.Context, prs []PullRequest) error
	UpsertCommits(ctx context.Context, commits []Commit) error
	UpsertTags(ctx context.Context, tags []Tag) error
	SampleOpenPullRequests(ctx context.Context, day time.Time) error

	// SelectRunsForJobFetch narrows a set of just-observed runs down to the
	// ones whose jobs are missing or stale.
	SelectRunsForJobFetch(ctx context.Context, candidates []RunJobSync) ([]int64, error)
	// MarkJobsSynced records that a run's jobs were fetched at that version of
	// the run. Written after the jobs are stored, never before.
	MarkJobsSynced(ctx context.Context, runID int64, syncKey time.Time) error
}

// DefaultJobBudgetPerPoll is how many jobs requests one poll cycle may issue,
// across all repositories.
//
// It is a budget for the whole cycle rather than per repository so that the
// bound does not grow with the number of repositories -- 25 today, and the
// failure being designed against was precisely a per-repository cost multiplied
// by the org. With the runs page, pull requests, commits and tags at one
// request per repository each, a poll now costs at most about 4*repos + budget
// requests, and with 25 repositories that is roughly 150: a shape that fits
// comfortably inside the secondary limiter instead of testing it.
//
// Anything beyond the budget is deferred, not dropped. The backfiller finds it
// by asking the database which runs are still missing jobs, so the only effect
// of a small budget is that a cold start fills in over hours instead of
// minutes -- which is the intended behavior, not a compromise.
const DefaultJobBudgetPerPoll = 50

// Poller orchestrates periodic collection of GitHub data across all repos in an org.
//
// It is now explicitly the STEADY-STATE half of collection: one page of recent
// workflow runs per repository, and jobs only for runs that have changed. The
// deep historical walk belongs to the Backfiller, which paces itself and may
// take hours. Keeping them apart is what lets the poll interval mean "how
// quickly new data appears" again, instead of "how often we re-download the
// past".
type Poller struct {
	client   *Client
	interval time.Duration
	org      string
	metrics  *Metrics

	// horizon is how far back a run may be and still be collected. See
	// horizon.go: it sits inside the retention window on purpose.
	horizon time.Duration
	// jobBudget bounds the jobs requests a single cycle may issue.
	jobBudget int

	repoCollector     *collectors.RepoCollector
	workflowCollector *collectors.WorkflowCollector
	prCollector       *collectors.PRCollector
	commitCollector   *collectors.CommitCollector
	tagCollector      *collectors.TagCollector

	nowFunc func() time.Time // for testing
}

// PollerOption adjusts a Poller at construction time.
type PollerOption func(*Poller)

// WithJobBudget sets how many jobs requests a single poll cycle may issue.
// Values below one are ignored: a poll that can never fetch a job would leave
// every run to the backfiller and make the interval meaningless.
func WithJobBudget(n int) PollerOption {
	return func(p *Poller) {
		if n > 0 {
			p.jobBudget = n
		}
	}
}

// WithHorizon overrides how far back runs are collected. Mainly for tests;
// production wants the default, which is derived from the retention window.
func WithHorizon(d time.Duration) PollerOption {
	return func(p *Poller) {
		if d > 0 {
			p.horizon = d
		}
	}
}

// NewPoller creates a new Poller that collects data every interval.
func NewPoller(
	client *Client, org string, interval time.Duration, metrics *Metrics, opts ...PollerOption,
) *Poller {
	p := &Poller{
		client:            client,
		interval:          interval,
		org:               org,
		metrics:           metrics,
		horizon:           CollectionHorizon,
		jobBudget:         DefaultJobBudgetPerPoll,
		repoCollector:     &collectors.RepoCollector{Client: client},
		workflowCollector: &collectors.WorkflowCollector{Client: client},
		prCollector:       &collectors.PRCollector{Client: client},
		commitCollector:   &collectors.CommitCollector{Client: client},
		tagCollector:      &collectors.TagCollector{Client: client},
		nowFunc:           time.Now,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Run starts the polling loop. It blocks until ctx is canceled.
func (p *Poller) Run(ctx context.Context, store StoreWriter) error {
	// Do an immediate poll on startup.
	p.poll(ctx, store)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			p.poll(ctx, store)
		}
	}
}

func (p *Poller) poll(ctx context.Context, store StoreWriter) {
	start := p.nowFunc()

	cRepos, err := p.repoCollector.CollectAll(ctx, p.org)
	if err != nil {
		log.Printf("error collecting repos: %v", err)
		p.metrics.ScrapeErrorsTotal.WithLabelValues("repos").Inc()
		return
	}

	repos := convertRepos(cRepos)
	if err := store.UpsertRepositories(ctx, repos); err != nil {
		log.Printf("error upserting repos: %v", err)
		p.metrics.ScrapeErrorsTotal.WithLabelValues("repos").Inc()
		return
	}
	p.metrics.LastSuccessTimestamp.WithLabelValues("repos").Set(float64(p.nowFunc().Unix()))

	budget := &jobBudget{remaining: p.jobBudget, metrics: p.metrics}
	for _, repo := range repos {
		p.collectRepo(ctx, store, repo, budget)
	}

	p.sampleOpenPullRequests(ctx, store)

	// Update rate limit metric.
	p.metrics.RateLimitRemaining.Set(float64(p.client.RateLimitRemaining()))

	duration := p.nowFunc().Sub(start).Seconds()
	p.metrics.PollDurationSeconds.Observe(duration)
}

// sampleOpenPullRequests writes the end-of-day open pull request count for
// today and for yesterday.
//
// Yesterday as well as today because nothing guarantees a poll lands near
// midnight, and the count for a day that nobody sampled cannot be recovered
// once the raw rows are pruned. Re-sampling a finished day is not a smear:
// the count is reconstructed from created_at and closed_at, so asking again
// tomorrow gives the same answer it would have given at the time.
//
// Failures are logged and the poll continues. A missing sample loses one day of
// one trend line; abandoning the poll would lose everything collected after it.
func (p *Poller) sampleOpenPullRequests(ctx context.Context, store StoreWriter) {
	now := p.nowFunc()
	for _, day := range []time.Time{now, now.AddDate(0, 0, -1)} {
		if err := store.SampleOpenPullRequests(ctx, day); err != nil {
			log.Printf("error sampling open pull requests for %s: %v",
				day.Format("2006-01-02"), err)
			p.metrics.ScrapeErrorsTotal.WithLabelValues("pullrequests").Inc()
			continue
		}
		p.metrics.LastSuccessTimestamp.WithLabelValues("rollups").Set(float64(now.Unix()))
	}
}

func (p *Poller) collectRepo(
	ctx context.Context, store StoreWriter, repo Repository, budget *jobBudget,
) {
	repoName := repo.Name

	p.collectWorkflows(ctx, store, repoName, budget)

	// Pull requests
	cPRs, err := p.prCollector.Collect(ctx, p.org, repoName)
	if err != nil {
		log.Printf("error collecting PRs for %s: %v", repoName, err)
		p.metrics.ScrapeErrorsTotal.WithLabelValues("pullrequests").Inc()
	} else {
		prs := convertPullRequests(cPRs)
		if upsertErr := store.UpsertPullRequests(ctx, prs); upsertErr != nil {
			log.Printf("error upserting PRs for %s: %v", repoName, upsertErr)
		}
		openCount := 0
		for _, pr := range prs {
			if pr.State == "open" {
				openCount++
			}
		}
		p.metrics.OpenPullRequests.WithLabelValues(repoName).Set(float64(openCount))
		p.metrics.LastSuccessTimestamp.WithLabelValues("pullrequests").Set(float64(p.nowFunc().Unix()))
	}

	// Commits
	cCommits, err := p.commitCollector.Collect(ctx, p.org, repoName, repo.DefaultBranch)
	if err != nil {
		log.Printf("error collecting commits for %s: %v", repoName, err)
		p.metrics.ScrapeErrorsTotal.WithLabelValues("commits").Inc()
	} else {
		commits := convertCommits(cCommits)
		if upsertErr := store.UpsertCommits(ctx, commits); upsertErr != nil {
			log.Printf("error upserting commits for %s: %v", repoName, upsertErr)
		}
		p.metrics.CommitsTotal.WithLabelValues(repoName).Add(float64(len(commits)))
		p.metrics.LastSuccessTimestamp.WithLabelValues("commits").Set(float64(p.nowFunc().Unix()))
	}

	// Tags
	cTags, err := p.tagCollector.Collect(ctx, p.org, repoName)
	if err != nil {
		log.Printf("error collecting tags for %s: %v", repoName, err)
		p.metrics.ScrapeErrorsTotal.WithLabelValues("tags").Inc()
	} else {
		tags := convertTags(cTags)
		if upsertErr := store.UpsertTags(ctx, tags); upsertErr != nil {
			log.Printf("error upserting tags for %s: %v", repoName, upsertErr)
		}
		p.metrics.LastSuccessTimestamp.WithLabelValues("tags").Set(float64(p.nowFunc().Unix()))
	}
}

// jobBudget bounds the jobs requests one poll cycle may issue, across every
// repository, and remembers whether it ran out so that the fact can be reported
// once rather than per repository.
type jobBudget struct {
	remaining int
	spent     bool
	metrics   *Metrics
}

// take claims one request from the budget. It reports false once the budget is
// gone, and counts that the poll deferred work -- exactly once per cycle,
// because the interesting fact is "this poll could not keep up", not how many
// times it noticed.
func (b *jobBudget) take() bool {
	if b.remaining > 0 {
		b.remaining--
		return true
	}
	if !b.spent {
		b.spent = true
		b.metrics.JobBudgetExhaustedTotal.Inc()
	}
	return false
}

// collectWorkflows collects the recent workflow runs of one repository, and the
// jobs of the runs that have actually changed.
//
// The shape here is the entire lesson of 2026-08-11, so it is worth stating
// plainly. Exactly one request fetches runs: the first page, newest first,
// bounded to the collection horizon. Not the whole history -- the history is
// the backfiller's job, at one request every couple of seconds, for as long as
// it takes. Then the store is asked which of those runs need their jobs, and
// only those are fetched, and only up to the cycle's budget. What the budget
// turns away is not lost; the backfiller finds it in the database.
//
// Before this, the same method walked every page of history and issued one jobs
// request per run found. One repository here has 163 runs; twenty-five
// repositories made it thousands of requests, back to back, every fifteen
// minutes, for data already on disk. GitHub answered 403 from its secondary
// rate limiter -- with 5247 of 5250 primary quota remaining -- and the first
// poll never finished.
func (p *Poller) collectWorkflows(
	ctx context.Context, store StoreWriter, repoName string, budget *jobBudget,
) {
	cutoff := p.nowFunc().Add(-p.horizon)

	page, err := p.workflowCollector.CollectRunsPage(ctx, p.org, repoName,
		collectors.RunQuery{Page: 1, CreatedSince: cutoff})
	p.metrics.APIRequestsTotal.WithLabelValues("poll", "runs_page").Inc()
	if err != nil {
		log.Printf("error collecting workflows for %s: %v", repoName, err)
		p.metrics.ScrapeErrorsTotal.WithLabelValues("workflows").Inc()
		return
	}

	// A 304 is a success with nothing to do: the first page of runs has not
	// changed since the last cycle, so no run on it can have changed either.
	if page.NotModified {
		p.metrics.LastSuccessTimestamp.WithLabelValues("workflows").Set(float64(p.nowFunc().Unix()))
		return
	}

	runs := convertWorkflowRuns(page.Runs)
	if err := store.UpsertWorkflowRuns(ctx, runs); err != nil {
		log.Printf("error upserting workflow runs for %s: %v", repoName, err)
		p.metrics.ScrapeErrorsTotal.WithLabelValues("workflows").Inc()
		return
	}
	for _, r := range runs {
		p.metrics.WorkflowRunsTotal.WithLabelValues(r.Repo, r.Workflow, r.Conclusion).Inc()
	}

	// The runs are upserted before the store is asked, so that a run whose
	// jobs are wanted is certain to exist for the jobs to reference -- and so
	// that a run the prune has just consumed is certain NOT to, which is how
	// the store declines to spend a request on jobs that could never be stored.
	candidates := make([]RunJobSync, 0, len(runs))
	syncKeys := make(map[int64]time.Time, len(runs))
	for _, r := range runs {
		key := r.SyncKey()
		candidates = append(candidates, RunJobSync{RunID: r.ID, SyncKey: key})
		syncKeys[r.ID] = key
	}

	needed, err := store.SelectRunsForJobFetch(ctx, candidates)
	if err != nil {
		log.Printf("error selecting runs needing jobs for %s: %v", repoName, err)
		p.metrics.ScrapeErrorsTotal.WithLabelValues("workflows").Inc()
		return
	}
	if skipped := len(candidates) - len(needed); skipped > 0 {
		p.metrics.JobFetchesSkippedTotal.Add(float64(skipped))
	}

	for _, runID := range needed {
		if !budget.take() {
			log.Printf("job budget exhausted; deferring %s to the backfiller", repoName)
			return
		}
		if !p.fetchJobsForRun(ctx, store, repoName, runID, syncKeys[runID]) {
			// One failure ends this repository's job work for the cycle. A
			// jobs request that has just failed is most likely a 403 from the
			// secondary limiter, and the response to being told to slow down
			// cannot be to immediately ask again -- that is the loop that
			// produced the incident. The backfiller will come back to it.
			return
		}
	}

	p.metrics.LastSuccessTimestamp.WithLabelValues("workflows").Set(float64(p.nowFunc().Unix()))
}

// fetchJobsForRun fetches, stores, and marks the jobs of a single run. It
// reports whether the caller should keep going.
//
// MarkJobsSynced is written last. If the process dies between storing the jobs
// and marking them, the run is simply selected again and one request is
// repeated; marking first would lose the jobs permanently and silently.
func (p *Poller) fetchJobsForRun(
	ctx context.Context, store StoreWriter, repoName string, runID int64, syncKey time.Time,
) bool {
	cJobs, err := p.workflowCollector.CollectJobs(ctx, p.org, repoName, runID)
	p.metrics.APIRequestsTotal.WithLabelValues("poll", "jobs").Inc()
	if err != nil {
		log.Printf("error collecting jobs for %s run %d: %v", repoName, runID, err)
		p.metrics.ScrapeErrorsTotal.WithLabelValues("workflows").Inc()
		return false
	}

	if err := store.UpsertWorkflowJobs(ctx, convertWorkflowJobs(cJobs)); err != nil {
		log.Printf("error upserting workflow jobs for %s run %d: %v", repoName, runID, err)
		p.metrics.ScrapeErrorsTotal.WithLabelValues("workflows").Inc()
		return false
	}

	if err := store.MarkJobsSynced(ctx, runID, syncKey); err != nil {
		log.Printf("error marking jobs synced for %s run %d: %v", repoName, runID, err)
		p.metrics.ScrapeErrorsTotal.WithLabelValues("workflows").Inc()
		return false
	}
	return true
}

// PollOnce performs a single poll cycle. Useful for testing.
func (p *Poller) PollOnce(ctx context.Context, store StoreWriter) {
	p.poll(ctx, store)
}

// Org returns the organization being polled.
func (p *Poller) Org() string {
	return p.org
}

// formatRepoFullName builds "org/repo" for logging.
func formatRepoFullName(org, repo string) string {
	return fmt.Sprintf("%s/%s", org, repo)
}

// Type conversion functions from collectors types to github package types.

func convertRepos(in []collectors.Repository) []Repository {
	out := make([]Repository, len(in))
	for i, r := range in {
		out[i] = Repository{
			ID:            r.ID,
			Name:          r.Name,
			DefaultBranch: r.DefaultBranch,
			Visibility:    r.Visibility,
			Archived:      r.Archived,
			UpdatedAt:     r.UpdatedAt,
		}
	}
	return out
}

func convertWorkflowRuns(in []collectors.WorkflowRun) []WorkflowRun {
	out := make([]WorkflowRun, len(in))
	for i, r := range in {
		out[i] = WorkflowRun{
			ID:           r.ID,
			Repo:         r.Repo,
			Workflow:     r.Workflow,
			Branch:       r.Branch,
			Conclusion:   r.Conclusion,
			Attempt:      r.Attempt,
			CreatedAt:    r.CreatedAt,
			RunStartedAt: r.RunStartedAt,
			UpdatedAt:    r.UpdatedAt,
		}
	}
	return out
}

func convertWorkflowJobs(in []collectors.WorkflowJob) []WorkflowJob {
	out := make([]WorkflowJob, len(in))
	for i, j := range in {
		out[i] = WorkflowJob{
			ID:          j.ID,
			RunID:       j.RunID,
			Name:        j.Name,
			Conclusion:  j.Conclusion,
			StartedAt:   j.StartedAt,
			CompletedAt: j.CompletedAt,
		}
	}
	return out
}

func convertPullRequests(in []collectors.PullRequest) []PullRequest {
	out := make([]PullRequest, len(in))
	for i, p := range in {
		out[i] = PullRequest{
			ID:        p.ID,
			Repo:      p.Repo,
			Number:    p.Number,
			State:     p.State,
			Author:    p.Author,
			CreatedAt: p.CreatedAt,
			MergedAt:  p.MergedAt,
			ClosedAt:  p.ClosedAt,
			UpdatedAt: p.UpdatedAt,
		}
	}
	return out
}

func convertCommits(in []collectors.Commit) []Commit {
	out := make([]Commit, len(in))
	for i, c := range in {
		out[i] = Commit{
			SHA:         c.SHA,
			Repo:        c.Repo,
			Branch:      c.Branch,
			Author:      c.Author,
			Message:     c.Message,
			CommittedAt: c.CommittedAt,
		}
	}
	return out
}

func convertTags(in []collectors.Tag) []Tag {
	out := make([]Tag, len(in))
	for i, t := range in {
		out[i] = Tag{
			Repo:      t.Repo,
			Name:      t.Name,
			TargetSHA: t.TargetSHA,
			CreatedAt: t.CreatedAt,
		}
	}
	return out
}
