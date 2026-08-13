package collectors

import "time"

// Repository represents a GitHub repository as returned by the API.
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
