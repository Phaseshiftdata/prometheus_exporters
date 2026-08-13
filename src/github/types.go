// Package github provides a GitHub API client, authentication, collectors,
// and polling infrastructure for the GitHub exporter.
package github

import "time"

// Repository represents a GitHub repository.
type Repository struct {
	ID            int64
	Name          string
	DefaultBranch string
	Visibility    string
	Archived      bool
	UpdatedAt     time.Time
}

// WorkflowRun represents a single GitHub Actions workflow run.
type WorkflowRun struct {
	ID           int64
	Repo         string
	Workflow     string
	Branch       string
	Conclusion   string
	Attempt      int
	CreatedAt    time.Time
	RunStartedAt *time.Time
	UpdatedAt    *time.Time
}

// WorkflowJob represents a single job within a workflow run.
type WorkflowJob struct {
	ID          int64
	RunID       int64
	Name        string
	Conclusion  string
	StartedAt   *time.Time
	CompletedAt *time.Time
}

// PullRequest represents a GitHub pull request.
type PullRequest struct {
	ID        int64
	Repo      string
	Number    int
	State     string
	Author    string
	CreatedAt time.Time
	MergedAt  *time.Time
	ClosedAt  *time.Time
	UpdatedAt *time.Time
}

// Commit represents a single Git commit.
type Commit struct {
	SHA         string
	Repo        string
	Branch      string
	Author      string
	Message     string
	CommittedAt time.Time
}

// Tag represents a Git tag, optionally associated with a GitHub release.
type Tag struct {
	Repo      string
	Name      string
	TargetSHA string
	CreatedAt *time.Time
}

// RunJobSync pairs a workflow run with the version of that run whose jobs are,
// or would be, stored.
//
// SyncKey is the run's updated_at, falling back to created_at when GitHub gave
// no updated_at. A run whose updated_at has not moved cannot have gained, lost
// or changed a job, so a matching key means the jobs request has nothing to
// tell us and is not sent -- which is the whole point, because it was one such
// request per run, thousands back to back, that tripped GitHub's secondary rate
// limit on 2026-08-11. The fallback matters: updated_at is nullable, and a NULL
// would compare equal to the never-fetched marker and quietly exclude the run
// from ever being fetched.
type RunJobSync struct {
	RunID   int64
	SyncKey time.Time
}

// SyncKey returns the value used to decide whether a run's jobs are current.
func (r WorkflowRun) SyncKey() time.Time {
	if r.UpdatedAt != nil {
		return *r.UpdatedAt
	}
	return r.CreatedAt
}
