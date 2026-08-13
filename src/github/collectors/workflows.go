package collectors

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// WorkflowCollector collects workflow run and job data from the GitHub Actions API.
//
// It used to expose a single Collect() that walked every page of a
// repository's run history and then issued one request per run for that run's
// jobs. That method is gone, and its absence is the point of this file.
//
// On 2026-08-11 it stopped the exporter dead. The first poll collected 25
// repositories and never finished: nine minutes in,
// github_exporter_poll_duration_seconds_count was still 0, `up` was 1, and
// nothing had been logged. A burst of requests had tripped GitHub's SECONDARY
// rate limit, which answers 403 on request RATE rather than on quota -- the
// primary limit was measured at 5247 of 5250 remaining while the exporter was
// stuck. One repository here has 163 runs; run pagination had no cap; the walk
// started again from page 1 every cycle. Thousands of requests, back to back,
// for data that was already in PostgreSQL.
//
// So the unit of collection is now one request. CollectRunsPage fetches exactly
// one page of runs and says whether another may exist; CollectJobs fetches the
// jobs of exactly one run. Deciding how many of those to issue, how far apart,
// and whether they are needed at all is the caller's business -- the poller
// keeps to a per-cycle budget and asks the store what it already has, and the
// backfiller spends one request per tick. A collector that cannot decide to
// issue a thousand requests cannot repeat 2026-08-11.
type WorkflowCollector struct {
	Client APIClient
}

// RunsPerPage is the page size asked of the runs endpoint. It is also how
// CollectRunsPage decides whether a further page may exist: a full page means
// maybe, a short page means no.
const RunsPerPage = 100

// RunQuery bounds a single request for a page of workflow runs.
//
// CreatedSince is the horizon. It is passed to GitHub as a `created` search
// qualifier so that the filtering happens at the far end, and it is ALSO
// applied here to what comes back -- see CollectRunsPage for why that
// belt-and-braces is not paranoia.
type RunQuery struct {
	Page         int
	CreatedSince time.Time
}

// RunPage is one page of workflow runs.
//
// More reports whether a further page may exist, judged from the size of the
// page GitHub actually returned rather than from the number of runs left after
// the horizon filter -- a page can be full of runs that are all too old, and
// stopping there would be stopping on the wrong evidence.
//
// NotModified reports an ETag hit. Those cost no quota, but they are still
// requests and still count toward the secondary limit, which is why nothing in
// this package treats "we have an ETag" as a reason to ask more often.
type RunPage struct {
	Runs        []WorkflowRun
	More        bool
	NotModified bool
	// DroppedBeyondHorizon counts runs discarded for being older than
	// CreatedSince. A non-zero value means GitHub's own filter let something
	// through and the local guard caught it.
	DroppedBeyondHorizon int
}

type apiRunsResponse struct {
	WorkflowRuns []apiWorkflowRun `json:"workflow_runs"`
}

type apiWorkflowRun struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	HeadBranch   string `json:"head_branch"`
	Conclusion   string `json:"conclusion"`
	RunAttempt   int    `json:"run_attempt"`
	CreatedAt    string `json:"created_at"`
	RunStartedAt string `json:"run_started_at"`
	UpdatedAt    string `json:"updated_at"`
}

type apiJobsResponse struct {
	Jobs []apiWorkflowJob `json:"jobs"`
}

type apiWorkflowJob struct {
	ID          int64  `json:"id"`
	RunID       int64  `json:"run_id"`
	Name        string `json:"name"`
	Conclusion  string `json:"conclusion"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
}

// CollectRunsPage fetches exactly one page of workflow runs. One page, one
// request, no loop.
//
// The horizon is enforced twice on purpose. GitHub is asked for
// `created:>=<date>`, and whatever it returns is filtered again here. The
// duplication buys a property that the rest of the design leans on: the
// exporter never offers the store a run from beyond the retention window.
// Migration 003's rollups ACCUMULATE -- a raw row reaches them exactly once, in
// the statement that deletes it -- so a run that has already been pruned and
// rolled up, if it were fetched and inserted again, would be counted a second
// time and the day's totals would quietly inflate. That was happening on every
// fifteen-minute cycle before this change, because the old walk re-fetched all
// of history every time. A query parameter is not the right place to keep a
// correctness guarantee that costs a day of dashboard history when it slips.
func (c *WorkflowCollector) CollectRunsPage(
	ctx context.Context, org, repo string, q RunQuery,
) (RunPage, error) {
	page := q.Page
	if page < 1 {
		page = 1
	}

	params := url.Values{}
	params.Set("per_page", strconv.Itoa(RunsPerPage))
	params.Set("page", strconv.Itoa(page))
	if !q.CreatedSince.IsZero() {
		params.Set("created", ">="+q.CreatedSince.UTC().Format("2006-01-02"))
	}
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/runs?%s",
		org, repo, params.Encode())

	var resp apiRunsResponse
	modified, err := c.Client.Get(ctx, endpoint, &resp)
	if err != nil {
		return RunPage{}, fmt.Errorf("fetching workflow runs page %d for %s: %w", page, repo, err)
	}
	if !modified {
		return RunPage{NotModified: true}, nil
	}

	out := RunPage{More: len(resp.WorkflowRuns) >= RunsPerPage}
	for _, r := range resp.WorkflowRuns {
		createdAt, _ := time.Parse(time.RFC3339, r.CreatedAt)
		if !q.CreatedSince.IsZero() && createdAt.Before(q.CreatedSince) {
			out.DroppedBeyondHorizon++
			continue
		}

		run := WorkflowRun{
			ID:         r.ID,
			Repo:       repo,
			Workflow:   r.Name,
			Branch:     r.HeadBranch,
			Conclusion: r.Conclusion,
			Attempt:    r.RunAttempt,
			CreatedAt:  createdAt,
		}
		if t, err := time.Parse(time.RFC3339, r.RunStartedAt); err == nil {
			run.RunStartedAt = &t
		}
		if t, err := time.Parse(time.RFC3339, r.UpdatedAt); err == nil {
			run.UpdatedAt = &t
		}
		out.Runs = append(out.Runs, run)
	}
	return out, nil
}

// CollectJobs fetches the jobs of exactly one workflow run.
//
// This is the request that multiplied. Callers are expected to have asked the
// store whether they need it at all: a run whose updated_at has not moved since
// its jobs were last fetched cannot have new ones, and the cheapest request is
// the one that is never sent. An ETag would make this call free of quota but
// not free of a request, and it was request count that produced the 403.
func (c *WorkflowCollector) CollectJobs(
	ctx context.Context, org, repo string, runID int64,
) ([]WorkflowJob, error) {
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/runs/%d/jobs",
		org, repo, runID)

	var resp apiJobsResponse
	if _, err := c.Client.Get(ctx, endpoint, &resp); err != nil {
		return nil, fmt.Errorf("fetching jobs for run %d: %w", runID, err)
	}

	jobs := make([]WorkflowJob, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		job := WorkflowJob{
			ID:         j.ID,
			RunID:      j.RunID,
			Name:       j.Name,
			Conclusion: j.Conclusion,
		}
		if t, err := time.Parse(time.RFC3339, j.StartedAt); err == nil {
			job.StartedAt = &t
		}
		if t, err := time.Parse(time.RFC3339, j.CompletedAt); err == nil {
			job.CompletedAt = &t
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}
