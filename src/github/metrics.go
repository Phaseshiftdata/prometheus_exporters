package github

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds all Prometheus metrics for the GitHub exporter.
//
// The backfill and request-accounting metrics below exist because of what
// 2026-08-11 looked like from the outside: nothing. The first poll had stalled
// on a 403 from GitHub's secondary rate limiter, and every signal the exporter
// published was consistent with a healthy, idle process -- `up` was 1,
// /metrics answered 200, and poll_duration_seconds_count sat at 0 without any
// way to tell "still working" from "wedged". Hours went into establishing a
// fact the process already knew.
//
// So the rule for everything added here is that the exporter must be able to
// say what it is doing while it is doing it: how many requests it has spent and
// on what, how much of the backlog is left, and whether it is deliberately
// holding back. A number that moves is the difference between patience and a
// page.
type Metrics struct {
	WorkflowRunsTotal    *prometheus.CounterVec
	OpenPullRequests     *prometheus.GaugeVec
	CommitsTotal         *prometheus.CounterVec
	RateLimitRemaining   prometheus.Gauge
	ScrapeErrorsTotal    *prometheus.CounterVec
	LastSuccessTimestamp *prometheus.GaugeVec
	PollDurationSeconds  prometheus.Histogram

	APIRequestsTotal          *prometheus.CounterVec
	RateLimitedTotal          *prometheus.CounterVec
	JobFetchesSkippedTotal    prometheus.Counter
	JobBudgetExhaustedTotal   prometheus.Counter
	BackfillPendingJobRuns    prometheus.Gauge
	BackfillReposComplete     prometheus.Gauge
	BackfillReposTotal        prometheus.Gauge
	BackfillThrottledTotal    *prometheus.CounterVec
	BackfillPaused            prometheus.Gauge
	BackfillLastStepTimestamp prometheus.Gauge
}

// NewMetrics creates and registers all GitHub exporter metrics.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		WorkflowRunsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "github_exporter_workflow_runs_total",
			Help: "Total number of workflow runs observed, by repo, workflow, and conclusion.",
		}, []string{"repo", "workflow", "conclusion"}),

		OpenPullRequests: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "github_exporter_open_pull_requests",
			Help: "Current number of open pull requests per repo.",
		}, []string{"repo"}),

		CommitsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "github_exporter_commits_total",
			Help: "Total number of commits observed per repo.",
		}, []string{"repo"}),

		RateLimitRemaining: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "github_exporter_rate_limit_remaining",
			Help: "GitHub API rate limit remaining.",
		}),

		ScrapeErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "github_exporter_scrape_errors_total",
			Help: "Total number of scrape errors by collector.",
		}, []string{"collector"}),

		LastSuccessTimestamp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "github_exporter_last_success_timestamp_seconds",
			Help: "Unix timestamp of last successful scrape by collector.",
		}, []string{"collector"}),

		PollDurationSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "github_exporter_poll_duration_seconds",
			Help:    "Duration of a complete poll cycle in seconds.",
			Buckets: prometheus.DefBuckets,
		}),

		// Requests spent, split by which activity spent them. The secondary
		// rate limit is a limit on requests per unit time, so this is the
		// series to look at when GitHub starts answering 403 -- the primary
		// quota gauge above was at 5247 of 5250 while the exporter was stuck,
		// and said nothing useful.
		APIRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "github_exporter_api_requests_total",
			Help: "GitHub API requests issued, by activity (poll, backfill) and kind.",
		}, []string{"activity", "kind"}),

		// Throttling, by which limit did it. These are different conditions
		// with different fixes and they were indistinguishable from the
		// outside: a secondary 403 arrives with X-RateLimit-Remaining: 0 even
		// when the primary quota is untouched, so the rate limit gauge above
		// says "exhausted" while /rate_limit says 8 used of 5250. Reading
		// secondary here means "we are sending them too fast"; reading primary
		// means "we are sending too many".
		RateLimitedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "github_exporter_rate_limited_total",
			Help: "403 responses from GitHub, by which rate limit produced them.",
		}, []string{"kind"}),

		// Requests NOT spent, because the store already had the answer. This
		// is the cold-start saving made visible: it should be small on the
		// first pass over a repository and dominate every pass after it.
		JobFetchesSkippedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "github_exporter_job_fetches_skipped_total",
			Help: "Workflow runs whose jobs were already stored and unchanged, so no request was made.",
		}),

		// A poll that reaches its per-cycle job budget has deferred work to the
		// backfiller rather than dropped it. Without this counter that decision
		// is invisible and looks like missing data.
		JobBudgetExhaustedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "github_exporter_job_budget_exhausted_total",
			Help: "Poll cycles that hit their job-request budget and deferred the rest to the backfiller.",
		}),

		BackfillPendingJobRuns: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "github_exporter_backfill_pending_job_runs",
			Help: "Stored workflow runs still waiting for their jobs to be fetched.",
		}),

		BackfillReposComplete: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "github_exporter_backfill_repos_complete",
			Help: "Repositories whose historical run pagination has reached the end.",
		}),

		BackfillReposTotal: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "github_exporter_backfill_repos_total",
			Help: "Repositories in the backfill rotation.",
		}),

		// Throttling is a decision, not a failure, and it has to be
		// distinguishable from one. A backfiller that is quiet because it is
		// protecting the rate limit and a backfiller that is quiet because it
		// is broken look identical without this.
		BackfillThrottledTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "github_exporter_backfill_throttled_total",
			Help: "Backfill ticks that deliberately issued no request, by reason.",
		}, []string{"reason"}),

		BackfillPaused: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "github_exporter_backfill_paused",
			Help: "1 while the backfiller is holding back to protect the rate limit, 0 otherwise.",
		}),

		// The heartbeat. Because the failure being designed against is silence,
		// something has to move on every tick even when there is no work: an
		// unchanging timestamp is the signal that the loop itself has stopped.
		BackfillLastStepTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "github_exporter_backfill_last_step_timestamp_seconds",
			Help: "Unix timestamp of the last backfill tick, whether or not it issued a request.",
		}),
	}

	reg.MustRegister(
		m.WorkflowRunsTotal,
		m.OpenPullRequests,
		m.CommitsTotal,
		m.RateLimitRemaining,
		m.ScrapeErrorsTotal,
		m.LastSuccessTimestamp,
		m.PollDurationSeconds,
		m.APIRequestsTotal,
		m.RateLimitedTotal,
		m.JobFetchesSkippedTotal,
		m.JobBudgetExhaustedTotal,
		m.BackfillPendingJobRuns,
		m.BackfillReposComplete,
		m.BackfillReposTotal,
		m.BackfillThrottledTotal,
		m.BackfillPaused,
		m.BackfillLastStepTimestamp,
	)

	return m
}
