// Package db provides PostgreSQL storage for GitHub exporter data.
// It includes schema migrations, an upsert-based store, and daily rollup
// aggregation with automatic retention pruning.
package db

import (
	"context"
	"time"
)

// DBPool abstracts the subset of pgxpool.Pool used by this package so that
// tests can supply a mock without requiring a live database.
type DBPool interface {
	Exec(ctx context.Context, sql string, arguments ...any) (CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
}

// CommandTag is a minimal interface matching pgx's CommandTag.
type CommandTag interface {
	RowsAffected() int64
}

// Row is a minimal interface matching pgx's Row.
type Row interface {
	Scan(dest ...any) error
}

// Rows is a minimal interface matching pgx's Rows.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}

// Repository represents a GitHub repository record.
type Repository struct {
	ID            int64
	Name          string
	DefaultBranch string
	Visibility    string
	Archived      bool
	UpdatedAt     *time.Time
}

// WorkflowRun represents a single CI workflow run.
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

// PullRequest represents a GitHub pull request record.
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

// Commit represents a single Git commit record.
type Commit struct {
	SHA         string
	Repo        string
	Branch      string
	Author      string
	Message     string
	CommittedAt time.Time
}

// Tag represents a Git tag record.
type Tag struct {
	ID        int64
	Repo      string
	Name      string
	TargetSHA string
	CreatedAt *time.Time
}
